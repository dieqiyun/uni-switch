package usage

import (
	"fmt"
	"maps"
	"testing"
	"time"
)

// A chunk's Codex ids are read once while its serial stands: a poll that
// folds the same session files again reads none of them, and folds them
// the same. A chunk made again (a live session's) is read again.
func TestFoldReadsAChunksIDsOnce(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	chunk := func(ids ...string) *rowChunk {
		c := &rowChunk{}
		for i, id := range ids {
			c.add(Row{Record: Record{Time: now.Add(-time.Duration(i) * time.Minute), Agent: "codex", Model: "native-model", Input: 80, Output: 8,
				Session: "native-session", ResponseID: id}, Source: "log"}, "", int64(i), false)
		}
		for i := range 200 { // enough for the archive to be smaller than the rows
			c.add(Row{Record: Record{Time: now.Add(-time.Hour), Agent: "claude", Model: "m", Session: "s", ResponseID: fmt.Sprint("other-", i)}}, "", int64(len(ids)+i), false)
		}
		packed := c.pack()
		if len(packed.Archive) == 0 {
			t.Fatal("the chunk wasn't packed")
		}
		packed.serial = nextChunkSerial()
		return packed
	}
	a, b := chunk("resp-1", "resp-2"), chunk("resp-1", "resp-3")
	var read []*rowChunk
	foldHashing = func(c *rowChunk) { read = append(read, c) }
	t.Cleanup(func() { foldHashing = nil })

	first := refMap(foldCodexRows([]*rowChunk{a, b}))
	if len(read) != 2 || len(first) != 1 || !first[rowRef{b, 0}] {
		t.Fatalf("first fold read %d chunks, skipped %v; want both read and b's resp-1 skipped", len(read), first)
	}
	read = nil
	again := refMap(foldCodexRows([]*rowChunk{a, b}))
	if len(read) != 0 {
		t.Fatalf("a fold of the same chunks read %d of them again", len(read))
	}
	if !maps.Equal(first, again) {
		t.Fatalf("the same chunks folded %v, then %v", first, again)
	}

	read = nil
	live := chunk("resp-1", "resp-4")
	folded := refMap(foldCodexRows([]*rowChunk{a, live}))
	if len(read) != 1 || read[0] != live {
		t.Fatalf("a chunk made again wasn't the one read: %d read", len(read))
	}
	if len(folded) != 1 || !folded[rowRef{live, 0}] {
		t.Fatalf("the live chunk's copy of a's call = %v", folded)
	}
}
