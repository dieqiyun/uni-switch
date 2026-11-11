package usage

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// A poll pairs the gateway's Codex calls with the session files' by
// response id without reading a file none of whose ids the gateway named:
// the ids kept for the fold say which chunks can hold one.
func TestPairsReadOnlyChunksTheGatewayNamed(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	chunk := func(source string, ids ...string) *rowChunk {
		c := &rowChunk{}
		for i, id := range ids {
			c.add(Row{Record: Record{Time: now.Add(-time.Duration(i) * time.Minute), Agent: "codex", Model: "native-model", Input: 80, Output: 8,
				Session: "native-session", ResponseID: id}, Source: source}, "", int64(i), false)
		}
		for i := range 200 { // enough for the archive to be smaller than the rows
			c.add(Row{Record: Record{Time: now.Add(-time.Hour), Agent: "claude", Model: "m", Session: "s", ResponseID: fmt.Sprint(source, "-other-", i)}}, "", int64(len(ids)+i), false)
		}
		packed := c.pack()
		if len(packed.Archive) == 0 {
			t.Fatal("the chunk wasn't packed")
		}
		packed.serial = nextChunkSerial()
		return packed
	}
	gateway := chunk("", "resp-1")
	named, other := chunk("log", "resp-2", "resp-1"), chunk("log", "resp-9")
	locals := []*rowChunk{named, other}
	foldCodexRows(locals) // a poll folds the chunks first
	var hashed, opened []*rowChunk
	foldHashing = func(c *rowChunk) { hashed = append(hashed, c) }
	pairsOpening = func(c *rowChunk) { opened = append(opened, c) }
	t.Cleanup(func() { foldHashing, pairsOpening = nil, nil })

	pairs, _ := codexChunkResponsePairs([]*rowChunk{gateway}, locals, nil)
	if len(hashed) != 0 {
		t.Fatalf("pairing read the ids of %d chunks again", len(hashed))
	}
	if len(opened) != 1 || opened[0] != named {
		t.Fatalf("pairing read %d local chunks; want only the one holding resp-1", len(opened))
	}
	if got, ok := pairs[rowRef{gateway, 0}]; !ok || got != (rowRef{named, 1}) {
		t.Fatalf("pairs = %v; want the gateway's resp-1 paired with the file's", pairs)
	}
}

// A response id's pick is a place in one slice, not an allocation of its
// own: a pick each was ~300 MB of what two minutes of polls allocated
// over a year's Codex calls while an agent worked (#2278). Every id named
// once on both sides is still paired.
func TestPairsAllocateNoPickAnID(t *testing.T) {
	const n = 4000
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	gateway, local := &rowChunk{}, &rowChunk{}
	for i := range n {
		r := Record{Time: now.Add(-time.Duration(i) * time.Second), Agent: "codex", Model: "native-model", Input: 80, Output: 8,
			Session: "native-session", ResponseID: fmt.Sprint("resp-", i)}
		gateway.add(Row{Record: r}, "", int64(i), false)
		local.add(Row{Record: r, Source: "log"}, "", int64(i), false)
	}
	local.serial = nextChunkSerial()
	locals := []*rowChunk{local}
	foldCodexRows(locals)
	pairs, _ := codexChunkResponsePairs([]*rowChunk{gateway}, locals, nil)
	if len(pairs) != n || pairs[rowRef{gateway, 7}] != (rowRef{local, 7}) {
		t.Fatalf("%d pairs of %d calls", len(pairs), n)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	codexChunkResponsePairs([]*rowChunk{gateway}, locals, nil)
	runtime.ReadMemStats(&after)
	if mallocs := after.Mallocs - before.Mallocs; mallocs > n/4 {
		t.Fatalf("pairing %d calls made %d allocations", n, mallocs)
	}
}
