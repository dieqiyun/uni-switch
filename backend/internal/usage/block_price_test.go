package usage

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Queries reading at once hold one priced copy of a gateway block between
// them, kept until the last is done: the GUI's requests at a cold start
// each held the whole history's, and the app doubled (#2278). A query of
// other prices or names prices its own.
func TestPricedBlocksSharedWhileHeld(t *testing.T) {
	var raw rowChunk
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < logBlockRows; i++ {
		raw.add(Row{Record: Record{Time: at, Provider: "openai", Model: "gpt-5", Input: 100 + i, Output: 10}}, "", int64(i), false)
	}
	packed := raw.pack()
	if packed == &raw {
		t.Fatal("the block was not packed")
	}
	pricer := func(meta string) *blockPricer {
		b := newBlockPricer(provider.Renames{}, nil, func(Record) *catalog.Price { return nil })
		b.share(sha256.Sum256([]byte(meta)))
		return b
	}
	held := func() int {
		sharedPriced.Lock()
		defer sharedPriced.Unlock()
		n := 0
		for k := range sharedPriced.blocks {
			if k.block == packed {
				n++
			}
		}
		return n
	}
	a, b, other := pricer("m"), pricer("m"), pricer("n")
	ca, cb, co := a.block(packed), b.block(packed), other.block(packed)
	if ca == nil || ca != cb {
		t.Fatalf("two queries of one meta priced %p and %p", ca, cb)
	}
	if co == ca {
		t.Fatal("a query of other prices read another's block")
	}
	if ca.row(7).Input != 107 || co.row(7).Input != 107 {
		t.Fatalf("rows read back %d, %d", ca.row(7).Input, co.row(7).Input)
	}
	a.release()
	if held() != 2 || b.block(packed) != cb {
		t.Fatalf("a block one query still holds was let go: %d held", held())
	}
	b.release()
	b.release() // a second release lets nothing go twice
	other.release()
	// the next poll's query reads the same priced block
	if n := held(); n != 2 {
		t.Fatalf("%d blocks kept after every query was done, want 2", n)
	}
	next := pricer("m")
	if c := next.block(packed); c != ca {
		t.Fatal("the next query of one meta priced the block again")
	}
	next.release()
	// kept until pricedLinger passes with no query holding them
	sharedPriced.Lock()
	sweepPriced(time.Now().Add(pricedLinger/2), false)
	sharedPriced.Unlock()
	if n := held(); n != 2 {
		t.Fatalf("%d blocks kept before pricedLinger passed, want 2", n)
	}
	sharedPriced.Lock()
	sweepPriced(time.Now().Add(pricedLinger+time.Second), false)
	sharedPriced.Unlock()
	if n := held(); n != 0 {
		t.Fatalf("%d blocks kept pricedLinger after every query was done", n)
	}
	if c := pricer("m").block(packed); c == ca {
		t.Fatal("a block let go was handed out again")
	}
}

// The window's rest lets every priced block no query holds go at once,
// and one a query still holds stays.
func TestPricedBlocksGoWithTheScratch(t *testing.T) {
	var raw rowChunk
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < logBlockRows; i++ {
		raw.add(Row{Record: Record{Time: at, Provider: "openai", Model: "gpt-5", Input: 100 + i, Output: 10}}, "", int64(i), false)
	}
	packed, other := raw.pack(), (&raw).pack()
	pricer := func() *blockPricer {
		b := newBlockPricer(provider.Renames{}, nil, func(Record) *catalog.Price { return nil })
		b.share(sha256.Sum256([]byte("scratch")))
		return b
	}
	idle, holding := pricer(), pricer()
	idle.block(packed)
	idle.release()
	held := holding.block(other)
	ReleaseScratch()
	sharedPriced.Lock()
	_, kept := sharedPriced.blocks[pricedKey{sha256.Sum256([]byte("scratch")), packed}]
	_, still := sharedPriced.blocks[pricedKey{sha256.Sum256([]byte("scratch")), other}]
	sharedPriced.Unlock()
	if kept {
		t.Fatal("ReleaseScratch kept a priced block no query held")
	}
	if !still || held.row(3).Input != 103 {
		t.Fatal("ReleaseScratch let go a block a query still held")
	}
	holding.release()
	ReleaseScratch()
}

// A turn of a cold start lets go of the matcher's scratch, so the next
// turn's read doesn't sit on it, and keeps the priced blocks the next
// turn's queries read again (#2278).
func TestMatchScratchGoesPricedBlocksStay(t *testing.T) {
	t.Cleanup(ReleaseScratch)
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	g, l := pairFixture(12, 9)
	ReleaseScratch()
	pairRows(All, now, g, l, nil, nil)
	if keptMatch.Load() == nil || keptPicks.Load() == nil {
		t.Fatal("nothing kept for the next call")
	}
	keepRows(new(rowBuffer))
	var raw rowChunk
	for i := 0; i < logBlockRows; i++ {
		raw.add(Row{Record: Record{Time: now, Provider: "openai", Model: "gpt-5", Input: 100 + i, Output: 10}}, "", int64(i), false)
	}
	packed := raw.pack()
	meta := sha256.Sum256([]byte("turn"))
	b := newBlockPricer(provider.Renames{}, nil, func(Record) *catalog.Price { return nil })
	b.share(meta)
	c := b.block(packed)
	b.release()
	ReleaseMatchScratch()
	if keptMatch.Load() != nil || keptPicks.Load() != nil || keptSeen.Load() != nil {
		t.Fatal("the matcher's scratch was kept")
	}
	for i := range keptRows {
		if keptRows[i].Load() != nil {
			t.Fatal("a row buffer was kept")
		}
	}
	next := newBlockPricer(provider.Renames{}, nil, func(Record) *catalog.Price { return nil })
	next.share(meta)
	defer next.release()
	if next.block(packed) != c {
		t.Fatal("the priced block went with the scratch")
	}
}
