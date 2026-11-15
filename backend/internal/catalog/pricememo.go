package catalog

import (
	"strings"
	"sync"
	"sync/atomic"
)

// pricedMemo keeps what PricedBy and MakerPriced found, for the load of
// Reset count gen. The GUI's provider list prices every model of every
// provider on each poll, and a model looked for by every spelling in
// every maker's catalog cost the poll more than anything else it did
// (most of a provider list's 50ms). What they find depends on the loaded
// catalog, the providers asked and the id, and nothing else, so it is
// kept until the next Reset.
var pricedMemo struct {
	sync.Mutex
	gen int
	by  map[string]pricedFound
}

type pricedFound struct {
	pr Price
	ok bool
}

// pricedMemoEntries bounds the memo: past it, it starts again.
const pricedMemoEntries = 1 << 15

// pricedRuns counts the look-ups pricedOnce did not have (a test reads it).
var pricedRuns atomic.Uint64

// pricedOnce is find's answer for the providers and id under kind, from
// the memo when it has it. find runs without the memo's lock held:
// PricedBy calls itself for a renamed id.
func pricedOnce(at int, kind byte, providers []string, id string, find func() (Price, bool)) (Price, bool) {
	var k strings.Builder
	n := 2 + len(id)
	for _, p := range providers {
		n += len(p) + 1
	}
	k.Grow(n) // one allocation, not one a doubling: it is made for every price asked
	k.WriteByte(kind)
	for _, p := range providers {
		k.WriteString(p)
		k.WriteByte(0)
	}
	k.WriteByte(1)
	k.WriteString(id)
	key := k.String()
	pricedMemo.Lock()
	if pricedMemo.gen == at && pricedMemo.by != nil {
		if f, ok := pricedMemo.by[key]; ok {
			pricedMemo.Unlock()
			return f.pr, f.ok
		}
	}
	pricedMemo.Unlock()
	pricedRuns.Add(1)
	pr, ok := find()
	pricedMemo.Lock()
	if pricedMemo.gen != at || pricedMemo.by == nil || len(pricedMemo.by) >= pricedMemoEntries {
		if pricedMemo.gen <= at {
			pricedMemo.gen, pricedMemo.by = at, map[string]pricedFound{}
		}
	}
	if pricedMemo.gen == at {
		pricedMemo.by[key] = pricedFound{pr, ok}
	}
	pricedMemo.Unlock()
	return pr, ok
}
