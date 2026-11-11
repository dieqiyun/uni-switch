package sessions

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// A scan of a small session file takes a buffer the size of the file, not
// 1 MiB: a start reads thousands of them, and their buffers were most of
// what it allocated (#2776).
func TestScanOfASmallFileTakesASmallBuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(`{"type":"user","n":1}`+"\n"+`{"type":"assistant","n":2}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const scans = 200
	lines := 0
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range scans {
		if _, err := scanAt(p, 0, nil, func(b []byte, _, _ int64) bool { lines++; return true }); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	if lines != 2*scans {
		t.Fatalf("read %d lines, want %d", lines, 2*scans)
	}
	// 16 KiB each, with room for the file handles and anything else
	if per := (after.TotalAlloc - before.TotalAlloc) / scans; per > 64<<10 {
		t.Fatalf("a scan of a two-line file allocated %d bytes", per)
	}
}

// A line longer than the buffer (the file grew after its size was read) is
// still handed over whole.
func TestScanReadsALineLongerThanItsBuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	size := scanBuffer(f, 0)
	f.Close()
	long := `{"text":"` + strings.Repeat("x", 3*size) + `"}`
	if err := os.WriteFile(p, []byte(long+"\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []int
	if _, err := scanAt(p, 0, nil, func(b []byte, _, _ int64) bool { got = append(got, len(b)); return true }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != len(long) || got[1] != 2 {
		t.Fatalf("lines %v, want [%d 2]", got, len(long))
	}
}

// Every other reader of a session file sizes its buffer as scanAt does:
// the calls readers of MiniMax Code, agy and WorkBuddy, dsh's packed
// frames and a search's first look each took 1 MiB per file read (#2776).
func TestReadersOfASmallFileTakeASmallBuffer(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	line := `{"type":"user","n":1}` + "\n"
	plain := write("s.jsonl", line+line)
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	framed := write("s.v3.jsonl.zstd", string(enc.EncodeAll([]byte(line+line), nil)))
	enc.Close()
	for _, c := range []struct {
		name string
		read func()
	}{
		{"minimax", func() { readMiniMaxCalls(file{path: plain, key: "minimax-code:s"}) }},
		{"agy", func() { readAgyCalls(file{path: plain, key: "agy:s"}) }},
		{"workbuddy", func() { readWorkBuddyCalls(file{agent: "workbuddy", path: plain, key: "workbuddy:s"}) }},
		{"frames", func() { scanFrames(framed, 0, 0, func([]byte) bool { return true }) }},
		{"search", func() { mayHold(context.Background(), plain, "absent") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			const reads = 50
			c.read() // anything a first read sets up once
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			for range reads {
				c.read()
			}
			runtime.ReadMemStats(&after)
			if per := (after.TotalAlloc - before.TotalAlloc) / reads; per > 256<<10 {
				t.Fatalf("a read of a two-line file allocated %d bytes", per)
			}
		})
	}
}

// scanAllocated is what one scan of the file at p allocated, and the
// lengths of the lines it handed over.
func scanAllocated(t *testing.T, p string) (uint64, []int) {
	t.Helper()
	var got []int
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := scanAt(p, 0, nil, func(b []byte, _, _ int64) bool { got = append(got, len(b)); return true }); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, got
}

// A line longer than the read buffer (a pasted screenshot's base64) is
// gathered into one buffer of its own size. Grown a buffer's worth at a
// time it allocated ~5 times its length, for each reader of each file, and
// a start of sessions holding images allocated GBs of it (#2515).
func TestScanGathersALongLineOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	long := `{"type":"user","data":"` + strings.Repeat("x", 8<<20) + `"}`
	if err := os.WriteFile(p, []byte(long+"\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alloc, got := scanAllocated(t, p)
	if len(got) != 2 || got[0] != len(long) || got[1] != 2 {
		t.Fatalf("lines %v, want [%d 2]", got, len(long))
	}
	// the line once and the 1 MiB read buffer, with room for both; grown
	// a chunk at a time it was ~38 MiB
	if want := uint64(len(long)) * 3 / 2; alloc > want {
		t.Fatalf("a scan of an %d-byte line allocated %d bytes, want at most %d", len(long), alloc, want)
	}
}

// A line past maxLine is stepped over without being gathered first.
func TestScanStepsOverATooLongLineUngathered(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	long := `{"type":"user","data":"` + strings.Repeat("x", maxLine+(4<<20)) + `"}`
	if err := os.WriteFile(p, []byte(long+"\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alloc, got := scanAllocated(t, p)
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("lines %v, want [2]", got)
	}
	// the 1 MiB read buffer and the look ahead, with room; gathered first
	// it was ~150 MiB
	if alloc > 8<<20 {
		t.Fatalf("a scan stepping over a %d-byte line allocated %d bytes", len(long), alloc)
	}
}

// Scans of big files share their read buffers: each took a new one of up
// to 1 MiB, and a live uni-switch's scans allocated 3.8 GB of them in 48
// minutes while ~157 MB was live.
func TestScansShareTheirBuffers(t *testing.T) {
	if raceOn {
		t.Skip("-race drops pooled buffers at random")
	}
	p := filepath.Join(t.TempDir(), "big.jsonl")
	line := `{"type":"assistant","text":"` + strings.Repeat("x", 4000) + `"}` + "\n"
	writeFile(t, p, strings.Repeat(line, 600)) // ~2.4 MB
	const scans = 50
	lines := 0
	scan := func() {
		if _, err := scanAt(p, 0, nil, func([]byte, int64, int64) bool { lines++; return true }); err != nil {
			t.Fatal(err)
		}
	}
	scan()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range scans {
		scan()
	}
	runtime.ReadMemStats(&after)
	if lines != 600*(scans+1) {
		t.Fatalf("read %d lines", lines)
	}
	// a GC between scans may let a pooled buffer go: room for a few
	if per := (after.TotalAlloc - before.TotalAlloc) / scans; per > 128<<10 {
		t.Fatalf("a scan of a 2.4 MB file allocated %d bytes", per)
	}
}

// The tests run with a buffer given back filled with garbage: what makes
// a line kept past its scan fail in them.
func TestALineKeptPastItsScanIsGarbageInTests(t *testing.T) {
	r, release := lineReader(bytes.NewReader([]byte("{\"a\":1}\n")), 16<<10)
	b, err := r.ReadSlice('\n')
	if err != nil {
		t.Fatal(err)
	}
	release()
	if !bytes.Equal(b, bytes.Repeat([]byte{0xFF}, len(b))) {
		t.Fatalf("a line kept past its scan reads %q", b)
	}
}

// A packed rollout's long line (a pasted screenshot in a prompt) has no
// length to read ahead, so it was grown a read buffer at a time and
// allocated ~3.8 times its size; a cold read of 120 real packed rollouts
// allocated 1.6 GB of 2.2 GB there. It is now gathered once, at its own
// size, and handed over whole; a line its head says no to is not gathered
// at all.
func TestScanGathersAPackedLongLineOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rollout.jsonl.zst")
	wanted := `{"type":"want","data":"` + strings.Repeat("x", 20<<20) + `"}`
	unwanted := `{"type":"skip","data":"` + strings.Repeat("y", 20<<20) + `"}`
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := enc.EncodeAll([]byte(wanted+"\n{}\n"+unwanted+"\n{}\n"), nil)
	enc.Close()
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	skip := func(b []byte) bool { return !bytes.Contains(b[:min(len(b), 64)], []byte(`"skip"`)) }
	none := func([]byte) bool { return false } // sets up the decoder a packed scan keeps
	scan := func(head func([]byte) bool) (uint64, []int) {
		var got []int
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if _, err := scanAt(p, 0, head, func(b []byte, _, _ int64) bool {
			if len(b) > 2 && !bytes.Equal(b, []byte(wanted)) {
				t.Fatalf("a %d-byte line handed over is not the one written", len(b))
			}
			got = append(got, len(b))
			return true
		}); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc, got
	}
	if _, got := scan(none); len(got) != 0 {
		t.Fatalf("lines %v handed over, want none", got)
	}
	alloc, got := scan(skip)
	if len(got) != 3 || got[0] != len(wanted) || got[1] != 2 || got[2] != 2 {
		t.Fatalf("lines %v, want [%d 2 2]", got, len(wanted))
	}
	// the line once at its size, and at worst once more into blocks the
	// pool let go, with 16 MiB for the decoder's window; grown a chunk at
	// a time it was ~107 MB past the window
	if most := uint64(len(wanted))*5/2 + 16<<20; alloc > most {
		t.Fatalf("a scan of a packed %d-byte line allocated %d bytes, want at most %d", len(wanted), alloc, most)
	}
}

// Blocks give back what they were given in order, across a block's end.
func TestBlocksKeepALineInOrder(t *testing.T) {
	var g blocks
	defer g.release()
	var want []byte
	for i, n := range []int{700 << 10, 900 << 10, 1, 1 << 20, 3} {
		p := bytes.Repeat([]byte{byte('a' + i)}, n)
		g.add(p)
		want = append(want, p...)
	}
	if got := g.appendTo([]byte("x")); !bytes.Equal(got, append([]byte("x"), want...)) || g.n != len(want) {
		t.Fatalf("blocks gave back %d bytes (n %d), not the %d given", len(got)-1, g.n, len(want))
	}
}
