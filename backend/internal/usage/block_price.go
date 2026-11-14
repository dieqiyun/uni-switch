package usage

import (
	"slices"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// swapKnown marks a row whose Swapped flag was settled when it was priced,
// so reading it back needs no second look at the two names.
const swapKnown = 64

// pricedAs is what a gateway call is priced as, the same for every call of
// one provider, model, served model and effort.
type pricedAs struct {
	provider string
	flags    uint16
	price    *catalog.Price
	// windows are the provider's rate windows (#2463): price is asked
	// without a time, so each row's own time is read against them
	windows []provider.RateWindow
}

// blockPricer prices the gateway's raw blocks as a query reads them. What a
// row's price, provider id, swap and routing are depends on a few of its
// names only, so they are worked out once for each set of names, not once
// per call: 750k calls name a few hundred.
type blockPricer struct {
	renames provider.Renames
	wires   map[string]string
	price   func(Record) *catalog.Price
	by      map[[4]string]pricedAs
	agents  map[string][2]string
	// meta names what prices and names a call (requestState.meta): the
	// blocks priced of it are shared with the other queries reading now
	// (sharedPriced). A pricer of none keeps its blocks to itself.
	meta   [32]byte
	shared bool
	held   []pricedKey
	pooled []*[]packedRow
}

// pricedRows are the rows of the blocks a query priced, handed back when
// it is done with them: a query over the whole history decodes every block,
// and allocating and clearing their rows again each time was a third of it.
var pricedRows = sync.Pool{New: func() any { s := make([]packedRow, 0, logBlockRows); return &s }}

type pricedKey struct {
	meta  [32]byte
	block *rowChunk
}

// pricedBlock is a gateway block priced, held by refs queries.
type pricedBlock struct {
	c    *rowChunk
	rows *[]packedRow // pooled, nil when c's rows are not
	refs int
	idle time.Time // when the last query holding it was done
}

// pricedLinger is how long a priced block is kept after the last query
// holding it is done. It outlasts the GUI's polls (Usage every 10 s,
// Sessions every 15 s): each poll priced the whole history again, its
// rows' pool emptied by the collections between, ~920 MB of 5.7 GB
// allocated in 80 s of polls (#2278). The window's rest lets them go at
// once (ReleaseScratch).
const pricedLinger = 30 * time.Second

// sharedPriced are the gateway's blocks the queries reading now priced, by
// block. A query over the whole history holds every block decoded until it
// is done, and the GUI's requests arriving together each held their own:
// at a cold start with the app open, three such queries held 105 MB of
// rows between them and the app grew to 737 MB where it was 388 MB unasked
// (#2278). A block is frozen once written (freeze), and its priced copy is
// only read after: a query that changes a row's thread clones the chunk
// first (cloneCodexChunks).
var sharedPriced = struct {
	sync.Mutex
	blocks map[pricedKey]*pricedBlock
	sweep  *time.Timer // the idle blocks' going, or nil
}{blocks: map[pricedKey]*pricedBlock{}}

// sweepPriced lets the blocks no query has held for pricedLinger go, all
// idle ones when every is true, and is armed again while idle ones are
// left. Called under sharedPriced's lock.
func sweepPriced(now time.Time, every bool) {
	left := time.Duration(0)
	for k, e := range sharedPriced.blocks {
		if e.refs != 0 {
			continue
		}
		if wait := pricedLinger - now.Sub(e.idle); !every && wait > 0 {
			left = max(left, wait)
			continue
		}
		delete(sharedPriced.blocks, k)
		if e.rows != nil {
			pricedRows.Put(e.rows)
		}
	}
	if sharedPriced.sweep != nil {
		sharedPriced.sweep.Stop()
		sharedPriced.sweep = nil
	}
	if left > 0 {
		sharedPriced.sweep = time.AfterFunc(left, func() {
			sharedPriced.Lock()
			defer sharedPriced.Unlock()
			sweepPriced(time.Now(), false)
		})
	}
}

// releasePriced lets every block no query holds now go.
func releasePriced() {
	sharedPriced.Lock()
	defer sharedPriced.Unlock()
	sweepPriced(time.Now(), true)
}

// share has b's blocks shared with the other queries of meta, a hash of
// requestState's root and meta.
func (b *blockPricer) share(meta [32]byte) {
	b.meta, b.shared = meta, true
}

// release hands the rows of the blocks it priced back, once the query no
// longer reads them (a page's rows are copies); a shared block's,
// pricedLinger after the last query holding it is done.
func (b *blockPricer) release() {
	for _, rows := range b.pooled {
		pricedRows.Put(rows)
	}
	b.pooled = nil
	if len(b.held) == 0 {
		return
	}
	sharedPriced.Lock()
	now := time.Now()
	for _, k := range b.held {
		e := sharedPriced.blocks[k]
		if e.refs--; e.refs == 0 {
			e.idle = now
		}
	}
	if sharedPriced.sweep == nil {
		sweepPriced(now, false)
	}
	sharedPriced.Unlock()
	b.held = nil
}

func newBlockPricer(renames provider.Renames, wires map[string]string, price func(Record) *catalog.Price) *blockPricer {
	return &blockPricer{renames: renames, wires: wires, price: price, by: map[[4]string]pricedAs{}, agents: map[string][2]string{}}
}

// as is priceRow's answer for a gateway call of these names, prov the
// id the provider has now without a merge (Renames.Own): what it is
// shown under does not change what it cost.
func (b *blockPricer) as(prov, model, served, effort string) pricedAs {
	k := [4]string{prov, model, served, effort}
	if a, ok := b.by[k]; ok {
		return a
	}
	a := pricedAs{provider: prov, flags: swapKnown}
	sent := provider.SentNameIn(b.wires, prov, model, effort)
	if served != "" && Swapped(sent, served) && !(SameSpelled(model, served) || ServingName(model, served) || RelayRenamed(model, served) || VendorAlias(model, served) || ListedAs(prov, model, served)) {
		a.flags |= 2
	}
	if GroupRouted(sent, served) {
		a.flags |= 32
	}
	a.price = b.price(Record{Provider: prov, Model: model, Served: served, Effort: effort})
	a.windows = provider.RateWindowsOf(prov)
	b.by[k] = a
	return a
}

// agent is reportedAs's answer for a client, the agent counted and the
// client's own name when the user's counting changed it.
func (b *blockPricer) agent(ua string) (string, string) {
	a, ok := b.agents[ua]
	if !ok {
		a[0], a[1] = reportedAs(ua)
		b.agents[ua] = a
	}
	return a[0], a[1]
}

// block is the priced copy of a raw block, held until release: one
// another query of the same meta priced and holds still, or one made now.
func (b *blockPricer) block(packed *rowChunk) *rowChunk {
	if !b.shared {
		c, rows := b.priceBlock(packed)
		if rows != nil {
			b.pooled = append(b.pooled, rows)
		}
		return c
	}
	k := pricedKey{b.meta, packed}
	sharedPriced.Lock()
	if e := sharedPriced.blocks[k]; e != nil {
		e.refs++
		sharedPriced.Unlock()
		b.held = append(b.held, k)
		return e.c
	}
	sharedPriced.Unlock()
	c, rows := b.priceBlock(packed)
	if c == nil {
		return nil
	}
	sharedPriced.Lock()
	e := sharedPriced.blocks[k]
	if e == nil { // priced by another query meanwhile: that one's is kept
		e = &pricedBlock{c: c, rows: rows}
		sharedPriced.blocks[k] = e
	} else if rows != nil {
		pricedRows.Put(rows)
	}
	e.refs++
	sharedPriced.Unlock()
	b.held = append(b.held, k)
	return e.c
}

// blockPricing, in tests, is told of each block priced.
var blockPricing func()

// priceBlock is the priced copy of a raw block, and the pooled rows it is in.
func (b *blockPricer) priceBlock(packed *rowChunk) (*rowChunk, *[]packedRow) {
	if blockPricing != nil {
		blockPricing()
	}
	buf := pricedRows.Get().(*[]packedRow)
	raw := packed.unpackInto(*buf)
	// unpackInto decodes into the buffer whenever it is large enough
	var pooled *[]packedRow
	if raw != nil && raw != packed && cap(*buf) >= len(raw.Rows) {
		pooled = buf
	} else {
		pricedRows.Put(buf)
	}
	if raw == nil {
		return nil, nil
	}
	c := *raw
	if raw == packed {
		c.Rows = slices.Clone(raw.Rows) // a block too small to compress is shared
	}
	c.raw = packed
	c.Strings = slices.Clip(c.Strings)
	added := map[string]uint32{}
	intern := func(s string, was uint32) uint32 {
		if c.Strings[was] == s {
			return was
		}
		if id, ok := added[s]; ok {
			return id
		}
		id := uint32(len(c.Strings))
		c.Strings = append(c.Strings, s)
		added[s] = id
		return id
	}
	// a shape's names are priced once, and the row takes the shape they
	// make: the agent, the client and the provider are all a shape's
	c.Shapes, c.shapeIDs = slices.Clip(c.Shapes), nil
	type ids struct {
		id      uint32 // the provider's string, or the row's shape
		flags   uint16
		price   *catalog.Price
		windows []provider.RateWindow
	}
	names := map[[6]uint32]ids{}
	agents := map[uint32][2]uint32{}
	shapes := map[[3]uint32]ids{}
	// the provider each shape names, as it is now: one an id was given
	// away from since has its calls told by when each was made, so its
	// rows take the shape per provider they go to (Renames.Timed)
	type renamedTo struct {
		to, own uint32 // shown under, and priced as (Renames.Both)
		timed   bool
	}
	provs := map[uint32]renamedTo{}
	for i := range c.Rows {
		p := &c.Rows[i]
		raw := c.Shapes[p.Shape][1]
		to, ok := provs[raw]
		if !ok {
			id := c.Strings[raw]
			to = renamedTo{timed: b.renames.Timed(id)}
			if !to.timed {
				shown, own := b.renames.Both(id, p.Time())
				to.to, to.own = intern(shown, raw), intern(own, raw)
			}
			provs[raw] = to
		}
		if to.timed {
			shown, own := b.renames.Both(c.Strings[raw], p.Time())
			to.to, to.own = intern(shown, raw), intern(own, raw)
		}
		sk := [3]uint32{p.Shape, to.to, to.own}
		got, ok := shapes[sk]
		if !ok {
			t := c.Shapes[p.Shape]
			k := [6]uint32{t[1], t[5], t[7], t[8], to.to, to.own}
			named, ok := names[k]
			if !ok {
				a := b.as(c.Strings[k[5]], c.Strings[k[1]], c.Strings[k[2]], c.Strings[k[3]])
				named = ids{k[4], a.flags, a.price, a.windows}
				names[k] = named
			}
			agent, ok := agents[t[0]]
			if !ok {
				a, client := b.agent(c.Strings[t[0]])
				agent = [2]uint32{intern(a, t[0]), intern(client, t[rowClient])}
				agents[t[0]] = agent
			}
			t[0], t[1], t[rowClient] = agent[0], named.id, agent[1]
			got = ids{c.shape(t), named.flags, named.price, named.windows}
			shapes[sk] = got
		}
		p.Shape = got.id
		p.Flags = p.Flags&^(1|2|32) | got.flags
		if p.Flags&rowOwnCost != 0 {
			// a reported call that said what it cost costs that (#1874)
			if p.Tokens[0]+p.Tokens[1] > 0 {
				p.Flags |= 1
			}
			continue
		}
		p.Cost = 0
		if got.price != nil && p.Tokens[0]+p.Tokens[1] > 0 {
			p.Flags |= 1
			p.Cost = atWindow(got.price, got.windows, p.Time()).In(p.Time()).CostSplit(int(p.Tokens[0]), int(p.Tokens[1]), int(p.Tokens[2]), int(p.Tokens[3]), int(p.Tokens[5]))
		}
	}
	return &c, pooled
}
