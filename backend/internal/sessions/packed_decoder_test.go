package sessions

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// A packed session file is decompressed through a decoder kept between
// reads, not one made for each. A decoder made fresh allocates its window
// (the Codex app packs rollouts with a 2 MiB one, which a low-memory
// decoder holds in 3 MiB) and its block buffers on every open: a live
// uni-switch's first read of 11,591 packed rollouts allocated 75 GB in zstd
// alone, 64.5 GB of it windows.

// packedWords is the text a fixture's lines are made of: varied enough
// that it packs about as well as a real rollout does.
var packedWords = strings.Fields(`the a session file read whole again when it changes go test run build error output line tool call model reply token cache input
	function package import return struct func var const if else for range map slice string byte int64 nil true false defer lock unlock
	assistant user message content text type payload timestamp response item event turn context compact usage effort medium high`)

// codexRollout writes a Codex rollout of about size bytes, as the Codex app
// writes it and then packs it: one frame with a 2 MiB window.
func codexRollout(t testing.TB, dir string, size int) string {
	t.Helper()
	r := rand.New(rand.NewSource(1))
	var b bytes.Buffer
	b.WriteString(`{"timestamp":"2026-09-20T07:48:28.000Z","type":"session_meta","payload":{"id":"01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd","cwd":"/Users/me/work/app","originator":"codex_cli_rs","cli_version":"0.99.0"}}` + "\n")
	text := func(n int) string {
		var s strings.Builder
		for s.Len() < n {
			s.WriteString(packedWords[r.Intn(len(packedWords))])
			s.WriteByte(' ')
		}
		return s.String()
	}
	for i := 0; b.Len() < size; i++ {
		ts := fmt.Sprintf("2026-09-20T%02d:%02d:%02d.%03dZ", 8+i/3600%12, i/60%60, i%60, i%1000)
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, `{"timestamp":%q,"type":"turn_context","payload":{"turn_id":"turn_%d","cwd":"/Users/me/work/app","model":"gpt-5.5","effort":"medium"}}`+"\n", ts, i)
		case 1:
			fmt.Fprintf(&b, `{"timestamp":%q,"type":"response_item","payload":{"type":"function_call_output","call_id":"call_%d","output":%q}}`+"\n", ts, i, text(2000+r.Intn(30000)))
		case 2:
			fmt.Fprintf(&b, `{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}}`+"\n", ts, text(200+r.Intn(2000)))
		case 3:
			fmt.Fprintf(&b, `{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":0,"total_tokens":%d},"last_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"output_tokens":%d,"reasoning_output_tokens":0,"total_tokens":%d}}}}`+"\n", ts, i*1000, i*900, i*10, i*1010, 1000, 10, 1010)
		}
	}
	var out bytes.Buffer
	w, err := zstd.NewWriter(&out, zstd.WithWindowSize(2<<20), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	w.Write(b.Bytes())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-2026-09-20T15-48-28-01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd.jsonl.zst")
	if err := os.WriteFile(p, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// dshPacked writes a dsh session of about size bytes, packed as dsh packs
// one: the header in a frame of its own, then a frame for each batch of
// events. It returns the file and where its last frame starts.
func dshPacked(t testing.TB, dir string, size int) (string, int64) {
	t.Helper()
	r := rand.New(rand.NewSource(2))
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	out := enc.EncodeAll([]byte(`{"type":"session","id":"session-4f1c2a9e","cwd":"/Users/me/work/app","createdAt":1790503200000}`+"\n"), nil)
	var last int64
	plain := 0
	for seq := 0; plain < size; {
		var batch bytes.Buffer
		for k := 0; k < 8; k++ {
			var s strings.Builder
			for n := 500 + r.Intn(8000); s.Len() < n; {
				s.WriteString(packedWords[r.Intn(len(packedWords))])
				s.WriteByte(' ')
			}
			fmt.Fprintf(&batch, `{"type":"assistant/message","seq":%d,"time":%d,"data":{"content":[{"type":"text","text":%q}],"message":{"source":{"model":"deepseek-v4-pro","provider":"deepseek"}},"usage":{"inputTokens":%d,"outputTokens":20,"cacheReadTokens":1000}}}`+"\n", seq, 1790503260000+int64(seq)*1000, s.String(), 100+seq)
			seq++
		}
		plain += batch.Len()
		last = int64(len(out))
		out = enc.EncodeAll(batch.Bytes(), out)
	}
	p := filepath.Join(dir, "session.v4.jsonl.zstd")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
	return p, last
}

// allocated is the bytes fn allocates a call, the least of n calls after
// a first one. TotalAlloc counts every goroutine in the process, so
// another test's leftover work lands on whichever call it overlaps; a
// decoder made anew lands on every call, and the least still shows it.
func allocated(n int, fn func()) uint64 {
	fn()
	least := ^uint64(0)
	for i := 0; i < n; i++ {
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		fn()
		runtime.ReadMemStats(&b)
		least = min(least, b.TotalAlloc-a.TotalAlloc)
	}
	return least
}

func scanPacked(t testing.TB, p string) int {
	lines := 0
	if _, err := scanAt(p, 0, nil, func(b []byte, _, _ int64) bool { lines++; return true }); err != nil {
		t.Fatal(err)
	}
	return lines
}

// A packed rollout read again allocates no window and no block buffers of
// its own: well under the 3 MiB its window alone took each time.
func TestPackedScanKeepsItsDecoder(t *testing.T) {
	if raceOn {
		t.Skip("-race lets a pool's decoders go")
	}
	p := codexRollout(t, t.TempDir(), 8<<20)
	want := scanPacked(t, p)
	if want < 1000 {
		t.Fatalf("the fixture has %d lines", want)
	}
	got := allocated(5, func() {
		if n := scanPacked(t, p); n != want {
			t.Fatalf("read %d lines, then %d", want, n)
		}
	})
	t.Logf("a scan of a packed rollout: %d bytes", got)
	if got > 512<<10 {
		t.Fatalf("a scan of a packed rollout allocated %d bytes: its decoder is made anew", got)
	}
}

// A dsh session's frames are decoded by a decoder kept between reads, and
// a read on from the last frame allocates about that frame, not a decoder.
func TestDshFramesKeepTheirDecoder(t *testing.T) {
	if raceOn {
		t.Skip("-race lets a pool's decoders go")
	}
	p, last := dshPacked(t, t.TempDir(), 6<<20)
	read := func(off int64) (lines int) {
		_, _, _, err := scanFrames(p, off, 0, func([]byte) bool { lines++; return true })
		if err != nil {
			t.Fatal(err)
		}
		return lines
	}
	if n := read(last); n != 8 {
		t.Fatalf("the last frame holds %d lines, want 8", n)
	}
	got := allocated(20, func() { read(last) })
	t.Logf("a read on from the last frame: %d bytes", got)
	if got > 128<<10 {
		t.Fatalf("a read on from the last frame allocated %d bytes: its decoder is made anew", got)
	}
}

// What a packed file's lines are, read through a decoder kept between
// reads, is what a decoder made for it alone reads: a file read after
// another, larger one, and one cut short, come out the same.
func TestPackedScanSameThroughAKeptDecoder(t *testing.T) {
	dir := t.TempDir()
	big := codexRollout(t, dir, 3<<20)
	small := filepath.Join(dir, "small.jsonl.zst")
	enc, _ := zstd.NewWriter(nil)
	os.WriteFile(small, enc.EncodeAll([]byte("{\"a\":1}\n{\"b\":2}\n{\"c\":"), nil), 0o644)
	enc.Close()
	whole := func(p string) []string {
		raw, _ := os.ReadFile(p)
		d, _ := zstd.NewReader(bytes.NewReader(raw))
		defer d.Close()
		var b bytes.Buffer
		b.ReadFrom(d)
		var out []string
		for _, l := range strings.SplitAfter(b.String(), "\n") {
			if strings.HasSuffix(l, "\n") && strings.TrimSpace(l) != "" {
				out = append(out, strings.TrimSpace(l))
			}
		}
		return out
	}
	read := func(p string, off int64) ([]string, int64) {
		var out []string
		end, err := scanAt(p, off, nil, func(b []byte, _, _ int64) bool { out = append(out, string(b)); return true })
		if err != nil {
			t.Fatal(err)
		}
		return out, end
	}
	for i := 0; i < 3; i++ {
		for _, p := range []string{big, small} {
			got, _ := read(p, 0)
			want := whole(p)
			if len(got) != len(want) {
				t.Fatalf("%s: %d lines, want %d", filepath.Base(p), len(got), len(want))
			}
			for j := range got {
				if got[j] != want[j] {
					t.Fatalf("%s: line %d differs", filepath.Base(p), j)
				}
			}
		}
	}
	// read on from a line's end, its places those of the lines decompressed
	all, end := read(big, 0)
	first, at := []string(nil), int64(0)
	scanAt(big, 0, nil, func(b []byte, _, e int64) bool { first, at = append(first, string(b)), e; return len(first) < 10 })
	rest, end2 := read(big, at)
	if end2 != end || len(first)+len(rest) != len(all) || rest[0] != all[10] {
		t.Fatalf("read on from %d: %d+%d lines to %d, want %d to %d", at, len(first), len(rest), end2, len(all), end)
	}
	// read at once by as many as usage's prefetch runs, each its own lines
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for _, p := range []string{big, small, big} {
				n := 0
				scanAt(p, 0, nil, func([]byte, int64, int64) bool { n++; return true })
				if want := len(whole(p)); n != want {
					errs <- fmt.Sprintf("reader %d: %s: %d lines, want %d", g, filepath.Base(p), n, want)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	// a read past the end hands back where it was asked to start
	if _, e := read(small, 1<<20); e != 1<<20 {
		t.Fatalf("past the end: %d", e)
	}
}

func BenchmarkPackedScan(b *testing.B) {
	p := codexRollout(b, b.TempDir(), 8<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanPacked(b, p)
	}
}

func BenchmarkDshFramesWhole(b *testing.B) {
	p, _ := dshPacked(b, b.TempDir(), 6<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanFrames(p, 0, 0, func([]byte) bool { return true })
	}
}

func BenchmarkDshFramesOn(b *testing.B) {
	p, last := dshPacked(b, b.TempDir(), 6<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanFrames(p, last, 0, func([]byte) bool { return true })
	}
}

// A packed rollout that hasn't changed (its size and time the same) is not
// decompressed again by the listing or by the calls read from it, while
// uni-switch runs or after it starts again from what it saved.
func TestUnchangedPackedNotReadAgain(t *testing.T) {
	_, codex := setup(t)
	var zs []string
	for _, p := range rollouts(t, codex) {
		zs = append(zs, pack(t, p, false))
	}
	c := &readCounter{n: map[string]int{}}
	scanHook = func(p string, _ int64) {
		if packed(p) {
			c.saw(p)
		}
	}
	t.Cleanup(func() { scanHook = nil })
	round := func() (int, int) {
		ss := List(0)
		srcs := CallSources()
		calls := 0
		for _, s := range srcs {
			calls += len(ReadCallSource(s))
		}
		return len(ss), calls
	}
	sessions, calls := round()
	if n := c.take(); len(n) != len(zs) {
		t.Fatalf("the first read decompressed %v, want each of %d rollouts", n, len(zs))
	}
	if calls == 0 {
		t.Fatal("no calls read from the packed rollouts")
	}
	for i, step := range []func(){func() {}, func() { Saved(); Reset() }, func() {}} {
		step()
		s, k := round()
		if n := c.take(); len(n) != 0 {
			t.Fatalf("round %d decompressed %v again", i+2, n)
		}
		if s != sessions || k != calls {
			t.Fatalf("round %d: %d sessions and %d calls, want %d and %d", i+2, s, k, sessions, calls)
		}
	}
	// one rollout changed: it alone is read again
	b, _ := os.ReadFile(zs[0])
	os.WriteFile(zs[0], b, 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(zs[0], later, later)
	round()
	if n := c.take(); len(n) != 1 || n[zs[0]] == 0 {
		t.Fatalf("after one changed, decompressed %v", n)
	}
}
