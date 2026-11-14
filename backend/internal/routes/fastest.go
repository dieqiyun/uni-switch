package routes

import (
	"sort"
	"sync"
)

// Speed first: of the same model on several providers, or of a
// provider's keys, the one that has answered fastest lately goes first
// (a user comparing API channels by hand asked for it, X joeliu96872540).
//
// What "fast" is: the gateway keeps, for each account or key and model, a
// decayed average of how long its streamed answers took to their first
// token and how fast they wrote after it (gateway speed.go), and hands
// the strategy those of the last hour or so as Seat.TTFT and Seat.TPS. A
// seat is ranked by the time it would take to a short reply of
// RefTokens: its first token, then RefTokens at its pace. Those within a
// tenth of the fastest of their band count as alike and keep their
// listed order, so two channels a few milliseconds apart don't trade
// places on every request.
//
// One not measured lately isn't starved: kept behind those measured, it
// would never answer and never be measured, and one that got slower an
// hour ago would never be found faster again. So one request in
// ProbeEvery, of those the pool has with both kinds awake, puts one not
// measured lately first: the probe's turn goes round them, so a seat
// that answers without telling its speed (a reply that isn't streamed)
// doesn't hold the turn from the others. When none is measured, they go
// in their order, and the first's answer measures it.
//
// Speed only orders the awake: one resting after a failure goes behind
// them, in its listed order, and one all but used up (UsedShare) behind
// those, as the other routings that read an allowance keep it; the
// gateway's rests and failover are as for any routing.

// RefTokens is the reply a seat's speed is weighed at: its first token,
// then this many at its pace. Short, since an agent's turns are mostly
// tool calls and short answers, where the first token is most of the wait.
const RefTokens = 200

// ProbeEvery: of a pool's requests with a seat measured and one not, one
// in this many gives the turn to one not measured lately.
const ProbeEvery = 5

// alike is the band within which seats count as alike: a tenth slower than
// the band's fastest.
const alike = 1.1

type probe struct {
	n     int // requests counted toward the next probe
	turns int // probes given: whose turn the next is
}

var probes = struct {
	sync.Mutex
	m map[string]probe // pool → its probes
}{m: map[string]probe{}}

// Fastest orders a pool by speed lately, as above.
var Fastest = Strategy{BySpeed: true, Order: func(pool string, seats []Seat) []int {
	var fast, unmeasured, resting, spent []int
	for i, s := range seats {
		switch {
		case s.Resting:
			resting = append(resting, i)
		case seatTier(s) == 2:
			spent = append(spent, i)
		case s.TTFT > 0:
			fast = append(fast, i)
		default:
			unmeasured = append(unmeasured, i)
		}
	}
	// a seat that told no pace counts at the middle pace of those that
	// did, so lacking it neither helps nor hurts; none told, the first
	// token alone decides
	var paces []float64
	for _, i := range fast {
		if seats[i].TPS > 0 {
			paces = append(paces, seats[i].TPS)
		}
	}
	mid := 0.0
	if len(paces) > 0 {
		sort.Float64s(paces)
		mid = paces[len(paces)/2]
	}
	est := make(map[int]float64, len(fast))
	for _, i := range fast {
		est[i] = Estimate(seats[i].TTFT, seats[i].TPS, mid)
	}
	sort.SliceStable(fast, func(a, b int) bool { return est[fast[a]] < est[fast[b]] })
	// bands drawn from the fastest down, then each band in its listed
	// order: sorting by "within a tenth" alone isn't consistent
	band := make(map[int]int, len(fast))
	top := 0.0
	for k, i := range fast {
		if k == 0 {
			top = est[i]
		} else if band[i] = band[fast[k-1]]; est[i] > alike*top {
			band[i]++
			top = est[i]
		}
	}
	sort.SliceStable(fast, func(a, b int) bool {
		if band[fast[a]] != band[fast[b]] {
			return band[fast[a]] < band[fast[b]]
		}
		return fast[a] < fast[b]
	})
	awake := append(fast, unmeasured...)
	if len(fast) > 0 && len(unmeasured) > 0 {
		probes.Lock()
		p := probes.m[pool]
		p.n++
		if p.n >= ProbeEvery {
			k := unmeasured[p.turns%len(unmeasured)]
			p.n, p.turns = 0, p.turns+1
			awake = append([]int{k}, deleted(awake, k)...)
		}
		probes.m[pool] = p
		probes.Unlock()
	}
	return append(append(awake, resting...), spent...)
}}

// Estimate is the ms to a reply of RefTokens at ttft and tps; a tps of 0
// counts as mid, and with mid 0 too the first token alone counts.
func Estimate(ttft, tps, mid float64) float64 {
	if tps <= 0 {
		tps = mid
	}
	if tps <= 0 {
		return ttft
	}
	return ttft + RefTokens*1000/tps
}

// deleted is l without x, in a new slice.
func deleted(l []int, x int) []int {
	out := make([]int, 0, len(l))
	for _, i := range l {
		if i != x {
			out = append(out, i)
		}
	}
	return out
}
