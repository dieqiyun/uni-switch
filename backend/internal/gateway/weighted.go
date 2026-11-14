package gateway

// Weighted routing (#841): a provider's requests spread over its keys by
// each key's weight, and a subscription's over its accounts by each
// account's (provider.AccountWeights, rubbzha on X), as NGINX's smooth
// weighted round robin does — keys
// weighing 3 and 1 go a, a, b, a, a, a, b, a…, the ratio held over a
// handful of requests rather than only on average. A key resting after a
// failure is left out of the round while it rests, so the others share
// its requests by their own weights and it takes up its share when it is
// back. A conversation still stays with the key that answered it while
// its affinity says (affinity.go); a request kept so counts against the
// key that took it, not the one the round picked (weightedKept), so new
// conversations make up the share.
//
// A group routed by weight (#1910) spreads its requests over its members
// the same way, by each member's weight (provider.Group.Weights): a
// member's own keys are ordered by its provider's routing, and a group in
// it is one member, planned by its own.

import (
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
)

// keyWeight is a candidate's weight: its account's (provider.AccountWeights,
// rubbzha on X) or its key's, 1 when none.
func keyWeight(c candidate) int {
	if c.p.Account != nil {
		return c.p.AccountWeight(c.p.Account.User)
	}
	return max(c.p.KeyWeight, 1)
}

// spentRest says whether c rests out of its quota or its credit, not for
// a rate limit or a failure: routes.Seat.Spent, what Drain goes on from.
func spentRest(c candidate) bool {
	r, ok := restingOf(c)
	return ok && (r.Why == failQuota || r.Why == failCredit)
}

// resting says whether c rests now, as restLast will find.
func resting(c candidate) bool {
	if _, ok := restOf(c.restKey()); ok {
		return true
	}
	if c.restID() != c.restKey() {
		_, ok := restOf(c.restID())
		return ok
	}
	return false
}

// weightedKept moves the pick of picked's round to kept, when affinity
// keeps a conversation with kept, another key of the same weighted
// provider, or another member of a group routed by weight that both are
// in: the round counts the requests each key, and each member, really
// took.
func weightedKept(picked, kept candidate) {
	if routingOf(picked.p.Routing) == provider.Weighted && picked.p.ID == kept.p.ID && picked.rest != kept.rest {
		routes.MoveRound(picked.p.ID, picked.rest, kept.rest)
	}
	for _, s := range picked.seats {
		for _, t := range kept.seats {
			if s.round == t.round && s.member != t.member {
				routes.MoveRound(s.round, s.member, t.member)
			}
		}
	}
}

// wrrSeat is a candidate's place in the round of a group routed by
// weight it is in: the round (the group's ref) and the member it is of.
type wrrSeat struct{ round, member string }
