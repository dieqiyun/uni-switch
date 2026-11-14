package usage

import (
	"slices"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// pairMemo is what pairRows found over one list of chunks, named by their
// serials: which rows another one stands for, and the Codex actors and
// compactions it gave each chunk. A page turned, or asked for again
// before anything changed, unpacked every session file three times over to
// pair the same rows again: about half of a warm page (#3493).
type pairMemo struct {
	since, until time.Time
	// serials are the chunks' in pairRows' order, each set ended by a 0;
	// a chunk without a serial (the probes') is named by its rows instead
	serials       []uint64
	skip, matched map[uint64][]uint64 // refSets, by serial
	actors        map[uint64]codexActors
	compactions   map[uint64]map[int]*bool
}

// pairSerials names the chunks of a pairing, or is false when two have
// no serial and so can't be told apart.
func pairSerials(sets ...[]*rowChunk) ([]uint64, bool) {
	var out []uint64
	unnamed := false
	for _, set := range sets {
		for _, c := range set {
			s := c.serial
			if s == 0 {
				if unnamed || len(c.Archive) != 0 {
					return nil, false
				}
				unnamed = true
				s = unnamedSerial(c)
			}
			out = append(out, s)
		}
		out = append(out, 0)
	}
	return out, true
}

// unnamedSerial stands for a chunk made again for each query, the probes':
// its rows are named by their place in the log and their time.
func unnamedSerial(c *rowChunk) uint64 {
	h := mix(6, uint64(len(c.Rows)))
	for i := range c.Rows {
		h = mix(h, uint64(c.Rows[i].Order), uint64(c.Rows[i].Time().UnixNano()))
	}
	return h | 1
}

// pairRowsMemo is pairRows, through old when it paired these chunks over
// the same window. The memo it returns is for the next call (nil: none).
func pairRowsMemo(p Period, now time.Time, gateways, chunks, others []*rowChunk, identities map[string]sessions.CodexIdentity, old *pairMemo) (*pairedRows, *pairMemo) {
	priceStubs(gateways)
	since, until := p.Since(now), p.Until(now)
	serials, ok := pairSerials(gateways, chunks, others)
	if !ok {
		return pairRows(p, now, gateways, chunks, others, identities), nil
	}
	if old != nil && old.since.Equal(since) && old.until.Equal(until) && slices.Equal(old.serials, serials) {
		return old.apply(serials, identities, gateways, chunks, others), old
	}
	ps := pairRows(p, now, gateways, chunks, others, identities)
	return ps, memoOf(ps, serials, append(append(slices.Clone(gateways), chunks...), others...))
}

// pairEvery is the pairing of every row of these chunks, as the whole
// history's page and Codex's effort pair them, through and for their memo
// (cache.pairs, All's), visited from since on. The heatmap paired its
// year apart: the same chunks for most histories, and a third pairing of
// all of them on a cold start beside the page's and the effort's, the
// last and highest of the footprint's peaks (#2278). A call is paired to
// its session row as the whole history pairs it; only what is counted is
// the window's.
func pairEvery(since, now time.Time, gateways, chunks, others []*rowChunk, identities map[string]sessions.CodexIdentity, cache *partCache) *pairedRows {
	ps, next := pairRowsMemo(All, now, gateways, chunks, others, identities, cache.pairs)
	if next != cache.pairs {
		cache.pairsNext = next
	}
	ps.since = since
	return ps
}

// memoOf is ps as a memo; originals are the chunks ps.all was cloned from.
func memoOf(ps *pairedRows, serials []uint64, originals []*rowChunk) *pairMemo {
	m := &pairMemo{since: ps.since, until: ps.until, serials: serials}
	named := make(map[*rowChunk]uint64, len(ps.all))
	k := 0
	for i, c := range ps.all {
		for serials[k] == 0 {
			k++
		}
		named[c] = serials[k]
		k++
		if !c.codexActors.equal(originals[i].codexActors) {
			if m.actors == nil {
				m.actors = map[uint64]codexActors{}
			}
			m.actors[serials[k-1]] = c.codexActors
		}
		if c.codexCompactions != nil {
			if m.compactions == nil {
				m.compactions = map[uint64]map[int]*bool{}
			}
			m.compactions[serials[k-1]] = c.codexCompactions
		}
	}
	refs := func(set refSet) map[uint64][]uint64 {
		out := make(map[uint64][]uint64, len(set))
		for c, b := range set {
			out[named[c]] = slices.Clone(b)
		}
		return out
	}
	m.skip, m.matched = refs(ps.skip), refs(ps.matched)
	return m
}

// apply is the pairing m remembers, on clones of these chunks.
func (m *pairMemo) apply(serials []uint64, identities map[string]sessions.CodexIdentity, sets ...[]*rowChunk) *pairedRows {
	var all []*rowChunk
	by := make(map[uint64]*rowChunk, len(serials))
	k := 0
	for _, set := range sets {
		for _, c := range cloneCodexChunks(set) {
			s := serials[k]
			k++
			if a, ok := m.actors[s]; ok {
				c.codexActors = a
			}
			c.codexCompactions = m.compactions[s]
			c.codexIdentities = identities
			by[s] = c
			all = append(all, c)
		}
		k++
	}
	refs := func(set map[uint64][]uint64) refSet {
		out := make(refSet, len(set))
		for s, b := range set {
			out[by[s]] = slices.Clone(b)
		}
		return out
	}
	return &pairedRows{all: all, skip: refs(m.skip), matched: refs(m.matched), since: m.since, until: m.until}
}
