package usage

import (
	"fmt"
	"maps"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The matcher keeps its maps and slices from one call for the next
// (#2278): a call after another, on other chunks or on the same ones,
// matches what a first call does.
func TestPairScratchKeepsNothingBetweenCalls(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	xg, xl := pairFixture(40, 30)
	yg, yl := pairFixture(12, 9)
	// what was matched, by chunk serial and row: Codex chunks are copied
	// for each call
	matched := func(g, l []*rowChunk) map[[2]uint64]bool {
		out := map[[2]uint64]bool{}
		for r := range pairRows(All, now, g, l, nil, nil).matched.all() {
			out[[2]uint64{r.Chunk.serial, uint64(r.Index)}] = true
		}
		return out
	}
	ReleaseScratch()
	fresh := matched(yg, yl)
	if len(fresh) != 12*9 {
		t.Fatalf("a first call matched %d of %d", len(fresh), 12*9)
	}
	matched(xg, xl)
	for round := range 2 {
		if got := matched(yg, yl); !maps.Equal(got, fresh) {
			t.Fatalf("round %d after another call matched %d, a first call %d", round, len(got), len(fresh))
		}
	}
	if keptMatch.Load() == nil || keptPicks.Load() == nil {
		t.Fatal("nothing kept for the next call")
	}
	ReleaseScratch()
	if keptMatch.Load() != nil || keptPicks.Load() != nil {
		t.Fatal("ReleaseScratch kept the scratch")
	}
}

// What one call reserved, used or looked up by id never holds in the next
// call's matching of the same calls.
func TestMatchScratchForgetsTheLastCall(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	call := func(id string, i int) Record {
		return Record{Time: now.Add(-time.Duration(i) * time.Minute), Agent: "claude", Provider: "relay", Session: "s", Input: 100 + i, Output: 5, Millis: 1000, RequestID: id}
	}
	chunk := func(source bool, rs ...Record) *rowChunk {
		c := &rowChunk{serial: nextChunkSerial()}
		for i, r := range rs {
			row := Row{Record: r}
			if source {
				c.Source, row.Source = sessions.CallSource{Path: fmt.Sprint("/local/", c.serial)}, "log"
			}
			c.add(row, "", int64(i), false)
		}
		return c
	}
	// the gateway's call req-a, and a local call that names it or one that
	// names no id, each the same call by session and tokens
	gateway := chunk(false, call("req-a", 0))
	named, unnamed := chunk(true, call("req-a", 0)), chunk(true, call("", 0))
	// another gateway, where req-a is the sixth id
	other := chunk(false, call("req-0", 9), call("req-1", 8), call("req-2", 7), call("req-3", 6), call("req-4", 5), call("req-a", 4))
	match := func(gateway, local *rowChunk, pairs map[rowRef]rowRef) bool {
		return matchedBlocks([]*rowChunk{gateway}, []*rowChunk{local}, nil, pairs, time.Time{}, false).has(rowRef{local, 0})
	}
	ReleaseScratch()
	if !match(gateway, unnamed, nil) || !match(gateway, named, nil) {
		t.Fatal("a first call left a local call unmatched")
	}
	// named's id used the gateway's call: unnamed still matches it after
	if !match(gateway, unnamed, nil) {
		t.Fatal("the last call's id match held the gateway's call")
	}
	// a response pair reserved the gateway's call
	match(gateway, unnamed, map[rowRef]rowRef{{gateway, 0}: {named, 0}})
	if !match(gateway, unnamed, nil) {
		t.Fatal("the last call's response pair held the gateway's call")
	}
	// req-a was the other gateway's sixth id
	match(other, chunk(true, call("req-a", 4)), nil)
	if !match(gateway, named, nil) {
		t.Fatal("the last call's ids were looked up")
	}
}

// The scratch a pairing kept goes once no pairing has used it for
// scratchLinger (#2278), and a pairing before then finds it kept.
func TestPairScratchGoesWhenUnused(t *testing.T) {
	was := scratchLinger
	scratchLinger = 2 * time.Second
	t.Cleanup(func() {
		scratchLinger = was
		ReleaseScratch()
	})
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	g, l := pairFixture(12, 9)
	ReleaseScratch()
	pairRows(All, now, g, l, nil, nil)
	kept := keptMatch.Load()
	if kept == nil || keptPicks.Load() == nil {
		t.Fatal("nothing kept for the next call")
	}
	pairRows(All, now, g, l, nil, nil)
	if keptMatch.Load() != kept {
		t.Fatal("a pairing right after made scratch of its own")
	}
	for deadline := time.Now().Add(30 * time.Second); keptMatch.Load() != nil || keptPicks.Load() != nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the scratch was kept with no pairing using it")
		}
	}
}

// A pairing much smaller than the one a scratch was made for makes its
// own, and the large one goes (#2278): a poll's pairing of its last days
// kept the whole history's ~140 MB at a cold start while the window polled.
// One alike in size takes what is kept.
func TestSmallPairingLetsLargeScratchGo(t *testing.T) {
	was := scratchFloor
	scratchFloor = 1
	t.Cleanup(func() {
		scratchFloor = was
		ReleaseScratch()
	})
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	bg, bl := pairFixture(40, 20)
	sg, sl := pairFixture(2, 3)
	ReleaseScratch()
	pairRows(All, now, bg, bl, nil, nil)
	match, picks, seen := keptMatch.Load(), keptPicks.Load(), keptSeen.Load()
	if match == nil || picks == nil || seen == nil {
		t.Fatal("nothing kept for the next call")
	}
	pairRows(All, now, bg, bl, nil, nil)
	if keptMatch.Load() != match || keptPicks.Load() != picks || keptSeen.Load() != seen {
		t.Fatal("a pairing alike in size made scratch of its own")
	}
	pairRows(All, now, sg, sl, nil, nil)
	if keptMatch.Load() == match {
		t.Fatal("a small pairing kept the match scratch of a large one")
	}
	if keptPicks.Load() == picks {
		t.Fatal("a small pairing kept the picks of a large one")
	}
	if keptSeen.Load() == seen {
		t.Fatal("a small pairing kept the ids seen of a large one")
	}
}

// A pass's decode buffer is kept for the next across collections, as the
// matcher's scratch is (#2278), and goes on ReleaseScratch.
func TestRowBufferKeptPastCollections(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	c := &rowChunk{}
	for i := range 300 {
		c.add(Row{Record: Record{Time: now.Add(-time.Duration(i) * time.Minute), Agent: "claude", Model: "m", Session: "s", Input: i, RequestID: fmt.Sprint("req-", i)}}, "", int64(i), false)
	}
	packed := c.pack()
	if len(packed.Archive) == 0 {
		t.Fatal("the chunk wasn't packed")
	}
	t.Cleanup(ReleaseScratch)
	ReleaseScratch()
	read := func() *rowBuffer {
		rows := lentRows()
		open := rows.of(packed)
		if len(open.Rows) != 300 || open.row(299).Input != 299 {
			t.Fatalf("read %d rows", len(open.Rows))
		}
		b := rows.b
		rows.release()
		return b
	}
	first := read()
	runtime.GC()
	runtime.GC()
	if got := read(); got != first {
		t.Fatal("a pass after a collection decoded into a new buffer")
	}
	// two passes at once each have one, and both are kept
	a, b := lentRows(), lentRows()
	a.of(packed)
	b.of(packed)
	if a.b == b.b {
		t.Fatal("two passes at once shared a buffer")
	}
	a.release()
	b.release()
	n := 0
	for i := range keptRows {
		if keptRows[i].Load() != nil {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d buffers kept after two passes", n)
	}
	ReleaseScratch()
	for i := range keptRows {
		if keptRows[i].Load() != nil {
			t.Fatal("ReleaseScratch kept a decode buffer")
		}
	}
}
