package usage

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// A packed block keeps its compressed bytes and no more. s2.Encode's
// output is as large as the worst case, more than the rows' own bytes,
// and an archive cut from it (even clipped) held that whole allocation:
// several times the compressed bytes Bytes counts, for as long as the
// block is cached.
func TestPackedArchiveHoldsOnlyItsBytes(t *testing.T) {
	now := time.Now()
	block := func(k, n int) *rowChunk {
		c := &rowChunk{}
		for i := range n {
			r := Row{Record: Record{Time: now.Add(time.Duration(i) * time.Second), Agent: "claude", Provider: "anthropic", Model: "claude-opus-5-5", Input: 1200 + i%7, Output: 300, Session: fmt.Sprint("s", k, "-", i/50)}}
			c.add(r, fmt.Sprint("msg", i), int64(i), false)
		}
		return c
	}
	var base, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)
	packed := make([]*rowChunk, 100)
	archives := 0
	for k := range packed {
		packed[k] = block(k, 2000).pack()
		if len(packed[k].Archive) == 0 {
			t.Fatalf("block %d: not packed", k)
		}
		archives += len(packed[k].Archive)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	// the unpacked rows went with the collection: what stays is the
	// archives, their chunks' headers and the scratch the pool keeps
	if held := int64(after.HeapAlloc) - int64(base.HeapAlloc); held > int64(archives)*3/2+1<<20 {
		t.Fatalf("100 packed blocks hold %d KB for %d KB of archives", held>>10, archives>>10)
	}
	for k, c := range packed {
		back := c.unpack()
		if back == nil || len(back.Rows) != 2000 || back.row(1999).Input != 1200+1999%7 || back.row(1999).Session != fmt.Sprint("s", k, "-", 1999/50) {
			t.Fatalf("block %d: not read back", k)
		}
	}
	// blocks packed one after another through the same scratch keep their
	// own bytes
	a, b := &rowChunk{}, &rowChunk{}
	for i := range 100 {
		a.add(Row{Record: Record{Time: now, Agent: "a", Model: fmt.Sprint("ma", i)}}, "", int64(i), false)
		b.add(Row{Record: Record{Time: now, Agent: "b", Model: fmt.Sprint("mb", i)}}, "", int64(i), false)
	}
	pa := a.pack()
	pb := b.pack()
	if ua, ub := pa.unpack(), pb.unpack(); ua.row(99).Model != "ma99" || ub.row(99).Model != "mb99" {
		t.Fatalf("read back %q and %q", ua.row(99).Model, ub.row(99).Model)
	}
}

// A block of a few rows is packed too: most session files hold a handful
// of calls, and a started uni-switch kept ~14k of them unpacked, ~98k rows
// and ~38 MB more than the same blocks read back from requests-v1.bin.
func TestFewRowsArePacked(t *testing.T) {
	now := time.Now()
	c := &rowChunk{}
	for i := range 3 {
		c.add(Row{Record: Record{Time: now.Add(time.Duration(i) * time.Second), Agent: "codex", Provider: "openai", Model: "gpt-5.5", Input: 1200 + i, Output: 300, Session: "s"}}, fmt.Sprint("msg", i), int64(i), false)
	}
	packed := c.pack()
	if len(packed.Archive) == 0 || packed.Rows != nil || packed.Bytes >= c.Bytes {
		t.Fatalf("3 rows: archive %d bytes, %d rows kept, %d bytes for %d", len(packed.Archive), len(packed.Rows), packed.Bytes, c.Bytes)
	}
	back := packed.unpack()
	if back == nil || len(back.Rows) != 3 || back.row(2).Input != 1202 || back.row(2).Model != "gpt-5.5" {
		t.Fatal("3 rows: not read back")
	}
}

// Blocks read one after another through one chunkRows reuse its rows',
// strings' and shapes' buffers, a large block's and then a small one's, and
// each reads back as it was packed: a row's text is the block's own, never
// a string or shape the block before left in the buffer.
func TestChunkRowsReuseReadsEachBlock(t *testing.T) {
	now := time.Now().Round(0)
	block := func(k, n int) (*rowChunk, *rowChunk) {
		c := &rowChunk{}
		for i := range n {
			r := Row{Record: Record{Time: now.Add(time.Duration(i) * time.Second), Agent: fmt.Sprint("agent", k, "-", i%3), Provider: "p", Model: fmt.Sprint("m", k, "-", i%5), Input: k*1000 + i, Session: fmt.Sprint("s", k, "-", i)}}
			c.add(r, fmt.Sprint("msg", k, "-", i), int64(i), i%4 == 0)
		}
		return c, c.pack()
	}
	sizes := []int{400, 3, 120, 1, 400}
	plains, packs := make([]*rowChunk, len(sizes)), make([]*rowChunk, len(sizes))
	for k, n := range sizes {
		plains[k], packs[k] = block(k, n)
	}
	// a pass after another reads through the buffers the last one released,
	// lent or not: in turn, and a large block after a small one
	for pass, lent := range []bool{false, true, false, true} {
		rows := chunkRows{lent: lent}
		for j := range packs {
			k := j
			if pass%2 == 1 {
				k = len(packs) - 1 - j
			}
			open := rows.of(packs[k])
			if len(open.Rows) != sizes[k] {
				t.Fatalf("pass %d block %d: %d rows, want %d", pass, k, len(open.Rows), sizes[k])
			}
			for i := range sizes[k] {
				if got, want := open.row(i), plains[k].row(i); !reflect.DeepEqual(got, want) {
					t.Fatalf("pass %d block %d row %d: read %+v, packed %+v", pass, k, i, got, want)
				}
			}
		}
		if cap(rows.b.strs) == 0 || cap(rows.b.shapes) == 0 {
			t.Fatalf("no buffers kept: %d strings, %d shapes", cap(rows.b.strs), cap(rows.b.shapes))
		}
		rows.release()
	}
}
