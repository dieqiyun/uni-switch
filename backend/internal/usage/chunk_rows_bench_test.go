package usage

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// benchChunks are n packed chunks of size rows each, every row with ids of
// its own, as a long history's session chunks are.
func benchChunks(b testing.TB, n, size int) []*rowChunk {
	b.Helper()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var chunks []*rowChunk
	for k := range n {
		c := &rowChunk{}
		for i := range size {
			at = at.Add(time.Second)
			c.add(Row{Record: Record{Time: at, Agent: "codex", Provider: "openai", Model: "gpt-5", Input: i, Session: fmt.Sprint("s", k), RequestID: fmt.Sprint("req-", k, "-", i)}}, fmt.Sprint("resp-", k, "-", i), int64(i), false)
		}
		packed := c.pack()
		if len(packed.Archive) == 0 {
			b.Fatal("not packed")
		}
		chunks = append(chunks, packed)
	}
	return chunks
}

// BenchmarkChunkRowsPass is one pass over a history's chunks, as a page's
// pass reads them: with its strings copied (visitBlocks) and lent.
func BenchmarkChunkRowsPass(b *testing.B) {
	chunks := benchChunks(b, 40, 3000)
	b.Run("copied", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			n := 0
			visitBlocks(chunks, time.Time{}, func(Record) { n++ })
			if n != 40*3000 {
				b.Fatal(n)
			}
		}
	})
	// a collection between passes, as polls while an agent works see:
	// what a pass keeps in a sync.Pool is gone by the next
	b.Run("copied after a collection", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			b.StopTimer()
			runtime.GC()
			runtime.GC()
			b.StartTimer()
			n := 0
			visitBlocks(chunks, time.Time{}, func(Record) { n++ })
			if n != 40*3000 {
				b.Fatal(n)
			}
		}
	})
	b.Run("lent", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rows := lentRows()
			n := 0
			for _, c := range chunks {
				n += len(rows.of(c).Rows)
			}
			rows.release()
			if n != 40*3000 {
				b.Fatal(n)
			}
		}
	})
}

// A pass that copies its strings decodes into the buffer passes keep, not
// a sync.Pool's, which each collection empties: after one, the pass made
// its decode buffer again, and grew it to the largest block (#4372). What
// it allocates is each block's strings, one copy a block.
func TestCopiedPassKeepsItsDecodeBuffer(t *testing.T) {
	chunks := benchChunks(t, 4, 2000)
	allocs := testing.AllocsPerRun(5, func() {
		runtime.GC()
		runtime.GC()
		visitBlocks(chunks, time.Time{}, func(Record) {})
	})
	if allocs > float64(len(chunks)) {
		t.Fatalf("a pass after a collection made %v allocations, want %d: one for each block's strings", allocs, len(chunks))
	}
}
