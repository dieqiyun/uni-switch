package sessions

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// A cached file's calls are read into a slice made for them, not grown
// call by call: append's doublings cost a warm start ~500 MB of garbage
// across every cached file, all of it in its first seconds.
func TestLoadedCallsAreSizedOnce(t *testing.T) {
	d := setupCalls(t)
	path := filepath.Join(d.claude, "projects", "p", "s.jsonl")
	st := &callFile{Path: path, Agent: "claude", Began: map[string]time.Time{}}
	add := func(n int) {
		for range n {
			i := len(st.Calls)
			st.Calls = append(st.Calls, Call{Time: time.Unix(int64(i), 0), Agent: "claude", Session: "s", Model: "claude-opus-5-5", Msg: fmt.Sprint("msg", i)})
		}
	}
	add(3000)
	writeCalls(st, 0, false)
	got := loadCalls(file{path: path, agent: "claude"})
	if got == nil || len(got.Calls) != 3000 || got.Calls[2999].Msg != "msg2999" || got.Calls[2999].File != path || got.Msgs["msg2999"] != 2999 {
		t.Fatal("cached calls not read back")
	}
	// made once, to the allocator's size class; grown by append it ends
	// a growth step past
	if cap(got.Calls)-len(got.Calls) > len(got.Calls)/100 {
		t.Fatalf("3000 calls read into a slice of %d", cap(got.Calls))
	}
	// a later frame appended to the file is read after the first, in order
	add(500)
	writeCalls(st, 3000, true)
	got = loadCalls(file{path: path, agent: "claude"})
	if got == nil || len(got.Calls) != 3500 || got.Calls[3499].Msg != "msg3499" || got.Calls[2999].Msg != "msg2999" {
		t.Fatal("appended calls not read back")
	}
}

// Shards are read one after another into one kept frame, and gob leaves a
// field of the zero value out: a call read after one that had a value
// there has none, not the last shard's.
func TestLoadedCallsKeepNothingOfTheLastShard(t *testing.T) {
	d := setupCalls(t)
	write := func(name string, c Call, began time.Time) string {
		path := filepath.Join(d.claude, "projects", "p", name+".jsonl")
		c.Msg = "msg-" + name
		st := &callFile{Path: path, Agent: "claude", Calls: []Call{c}, Began: map[string]time.Time{}}
		if !began.IsZero() {
			st.Began[c.Msg] = began
		}
		writeCalls(st, 0, false)
		return path
	}
	full := write("full", Call{Time: time.Unix(100, 0), Agent: "claude", Session: "s1", Model: "claude-opus-5-5",
		Tokens: Tokens{Input: 50, Output: 7, CacheRead: 9}, Compaction: true}, time.Unix(90, 0))
	bareCall := Call{Time: time.Unix(200, 0), Agent: "claude"}
	empty := write("empty", bareCall, time.Time{})
	for range 3 {
		if got := loadCalls(file{path: full, agent: "claude"}); got == nil || got.Calls[0].Input != 50 || !got.Calls[0].Compaction {
			t.Fatal("the full shard not read back")
		}
		got := loadCalls(file{path: empty, agent: "claude"})
		if got == nil || len(got.Calls) != 1 {
			t.Fatal("the bare shard not read back")
		}
		want := bareCall
		want.Msg, want.File = "msg-empty", empty
		if c := got.Calls[0]; c.Input != 0 || c.Output != 0 || c.CacheRead != 0 || c.Compaction || c.Session != "" || c.Model != "" || c.Msg != want.Msg || !c.Time.Equal(want.Time) {
			t.Fatalf("read after the full shard: %+v", c)
		}
		if b, ok := got.Began["msg-empty"]; ok {
			t.Fatalf("began %v kept from the last shard", b)
		}
	}
}

// A write is cut into frames of framePatches, read back as one (#3493); a
// file cut off after one of them is no file, as a torn frame is; and a
// file written as one large frame before is read whole and written again
// in frames.
func TestCallsWrittenInFrames(t *testing.T) {
	d := setupCalls(t)
	path := filepath.Join(d.claude, "projects", "p", "s.jsonl")
	st := &callFile{Path: path, Agent: "claude", Off: 77, Began: map[string]time.Time{}}
	n := 2*framePatches + 100
	for i := range n {
		msg := fmt.Sprint("msg", i)
		st.Calls = append(st.Calls, Call{Time: time.Unix(int64(i), 0), Agent: "claude", Session: "s", Model: fmt.Sprint("m", i%3), Msg: msg})
		st.Began[msg] = time.Unix(int64(i)-1, 0)
	}
	frames := func() (sizes []int) {
		b, err := os.ReadFile(callCachePath(path))
		if err != nil {
			t.Fatal(err)
		}
		for len(b) >= 8 {
			size := int(binary.LittleEndian.Uint32(b))
			var f callFrame
			if decodeFrame(b[8:8+size], &f) != nil {
				t.Fatal("a frame doesn't decode")
			}
			sizes = append(sizes, len(f.Patches))
			b = b[8+size:]
		}
		return sizes
	}
	check := func(what string) {
		t.Helper()
		got := loadKept(file{path: path, agent: "claude"})
		if got == nil || len(got.Calls) != n || got.Off != 77 {
			t.Fatalf("%s: not read back", what)
		}
		for i, c := range got.Calls {
			if c.Msg != fmt.Sprint("msg", i) || c.Model != fmt.Sprint("m", i%3) || !c.Time.Equal(time.Unix(int64(i), 0)) || c.File != path {
				t.Fatalf("%s: call %d is %+v", what, i, c)
			}
		}
		if !got.Began["msg4000"].Equal(time.Unix(3999, 0)) || got.Msgs["msg4000"] != 4000 {
			t.Fatalf("%s: messages not read back", what)
		}
		if cap(got.Calls)-len(got.Calls) > len(got.Calls)/100 {
			t.Fatalf("%s: %d calls read into a slice of %d", what, n, cap(got.Calls))
		}
	}
	writeCalls(st, 0, false)
	if got := frames(); !slices.Equal(got, []int{framePatches, framePatches, 100}) {
		t.Fatalf("frames of %v patches", got)
	}
	check("cut")

	// cut off after the first frame
	b, _ := os.ReadFile(callCachePath(path))
	first := 8 + int(binary.LittleEndian.Uint32(b))
	if err := os.WriteFile(callCachePath(path), b[:first], 0600); err != nil {
		t.Fatal(err)
	}
	if loadCalls(file{path: path, agent: "claude"}) != nil {
		t.Fatal("a file cut off mid-write read as whole")
	}

	// as written before frames were cut: one frame of every patch
	old := callFrame{State: diskCallState(st), Strings: []string{""}}
	dict := map[string]uint32{"": 0}
	for i := range st.Calls {
		p := callPatch{Index: i, Call: st.Calls[i], Began: st.Began[st.Calls[i].Msg]}
		for j, field := range callText(&p.Call) {
			id, ok := dict[*field]
			if !ok {
				id = uint32(len(old.Strings))
				dict[*field] = id
				old.Strings = append(old.Strings, *field)
			}
			p.Text[j] = id
			*field = ""
		}
		old.Patches = append(old.Patches, p)
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(old); err != nil {
		t.Fatal(err)
	}
	var header [8]byte
	binary.LittleEndian.PutUint32(header[:4], uint32(data.Len()))
	binary.LittleEndian.PutUint32(header[4:], crc32.ChecksumIEEE(data.Bytes()))
	if err := os.WriteFile(callCachePath(path), append(header[:], data.Bytes()...), 0600); err != nil {
		t.Fatal(err)
	}
	if got := frames(); !slices.Equal(got, []int{n}) {
		t.Fatalf("old file: frames of %v patches", got)
	}
	check("one large frame")
	if got := frames(); !slices.Equal(got, []int{framePatches, framePatches, 100}) {
		t.Fatalf("one large frame written again as frames of %v patches", got)
	}
	check("written again")
}
