package routes

import "sync"

// Weighted rounds (#841), as NGINX's smooth weighted round robin: keys
// weighing 3 and 1 go a, a, b, a, a, a, b, a…, the ratio held over a
// handful of requests rather than only on average. One resting is left out
// of the round while it rests, so the others share its requests by their
// own weights and it takes up its share when it is back. A provider routed
// by weight keeps one over its keys, a group routed by weight one over its
// members (#1910).

var rounds = struct {
	sync.Mutex
	m map[string]*round // pool → its round
}{m: map[string]*round{}}

type round struct {
	cur   map[string]int // a key, or a group's member → current weight
	total int
	last  string // the one the last pick was charged to
}

// Round is which of keys pool's round picks now, each weighing its
// weight, those resting left out of the round; -1 when every one rests.
// The pick is charged the round's total.
func Round(pool string, keys []string, weights []int, rests []bool) int {
	rounds.Lock()
	defer rounds.Unlock()
	r := rounds.m[pool]
	if r == nil {
		r = &round{cur: map[string]int{}}
		rounds.m[pool] = r
	}
	// keys no longer on are dropped from the round
	on := make(map[string]bool, len(keys))
	for _, k := range keys {
		on[k] = true
	}
	for k := range r.cur {
		if !on[k] {
			delete(r.cur, k)
		}
	}
	best, total := -1, 0
	for i, k := range keys {
		if rests[i] {
			continue
		}
		r.cur[k] += weights[i]
		total += weights[i]
		if best < 0 || r.cur[k] > r.cur[keys[best]] {
			best = i
		}
	}
	if best < 0 {
		return -1
	}
	r.cur[keys[best]] -= total
	r.total, r.last = total, keys[best]
	return best
}

// MoveRound charges pool's last pick to kept rather than picked, when
// affinity kept a conversation with kept: the round counts the requests
// each really took, so new conversations make up the share.
func MoveRound(pool, picked, kept string) {
	rounds.Lock()
	defer rounds.Unlock()
	r := rounds.m[pool]
	if r == nil || r.last != picked {
		return
	}
	if _, ok := r.cur[kept]; !ok {
		return
	}
	r.cur[picked] += r.total
	r.cur[kept] -= r.total
	r.last = kept
}

// Forget drops what pool's round, turn and drain hold, every pool's with none
// named: a test starts each anew.
func Forget(pools ...string) {
	rounds.Lock()
	turns.Lock()
	probes.Lock()
	drains.Lock()
	defer rounds.Unlock()
	defer drains.Unlock()
	defer turns.Unlock()
	defer probes.Unlock()
	if len(pools) == 0 {
		rounds.m, turns.m, probes.m, drains.m = map[string]*round{}, map[string]int{}, map[string]probe{}, map[string]drainAt{}
	}
	for _, p := range pools {
		delete(rounds.m, p)
		delete(turns.m, p)
		delete(probes.m, p)
		delete(drains.m, p)
	}
}
