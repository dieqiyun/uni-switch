package routes

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// dzxx0496 on Discord: free accounts one after another, each until its
// quota is used up, and the first not taking the requests back when its
// quota renews. Drain stays on the one it went on to; In order and Smart
// go back to the first.
func TestDrainStaysOnTheNextWhenTheFirstRenews(t *testing.T) {
	Forget()
	t.Cleanup(func() { Forget() })
	const pool = "workbuddy"
	order := func(seats ...Seat) []int { return Apply(Drain, pool, seats) }
	a, b, c := Seat{Key: "a", Known: true}, Seat{Key: "b", Known: true}, Seat{Key: "c", Known: true}

	if got := order(a, b, c); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("fresh: %v, want the first first", got)
	}
	// a used up: b, then c, then a for failover
	spentA := a
	spentA.Used = 100
	if got := order(spentA, b, c); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("a used up: %v, want b first", got)
	}
	// a renews: the pool stays on b
	if got := order(a, b, c); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("a renewed: %v, want b still first", got)
	}
	// asked again with nothing changed (a preview): the same
	if got := order(a, b, c); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("asked again: %v", got)
	}
	// b refused for its quota (resting, Spent): on to c, and it stays there
	// once b's rest is over
	spentB := b
	spentB.Resting, spentB.Spent = true, true
	if got := order(a, spentB, c); !slices.Equal(got, []int{2, 0, 1}) {
		t.Fatalf("b out of quota: %v, want c first", got)
	}
	if got := order(a, b, c); !slices.Equal(got, []int{2, 0, 1}) {
		t.Fatalf("b back: %v, want c still first", got)
	}
	// c used up: round to a, which renewed
	spentC := c
	spentC.Used = 100
	if got := order(a, b, spentC); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("c used up: %v, want a first again", got)
	}

	// what the other routings do with the same: back to the first
	if got := Apply(Ordered, pool, []Seat{a, b, c}); got[0] != 0 {
		t.Fatalf("In order: %v", got)
	}
}

// A rate limit or a failure rests a seat without using it up: the gateway
// tries the others meanwhile (restLast), and Drain stays on it.
func TestDrainStaysThroughARateLimit(t *testing.T) {
	Forget()
	t.Cleanup(func() { Forget() })
	const pool = "group/free"
	a, b := Seat{Key: "a", Known: true}, Seat{Key: "b", Known: true}
	limited := a
	limited.Resting = true
	if got := Apply(Drain, pool, []Seat{limited, b}); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("a rate limited: %v, want it kept first", got)
	}
	// 98% is not used up: In order's line (#530); a key's Used, not an
	// allowance, never counts
	key := Seat{Key: "a", Used: 100}
	if got := Apply(Drain, pool, []Seat{key, b}); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("a key: %v, want it kept first", got)
	}
	low := a
	low.Used = provider.SpentShare
	if got := Apply(Drain, pool, []Seat{low, b}); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("a at %v%%: %v, want it kept first", provider.SpentShare, got)
	}
}

// Every seat used up: it stays where it is. The seat it was on gone from
// the pool: the first not used up.
func TestDrainAllSpentAndGone(t *testing.T) {
	Forget()
	t.Cleanup(func() { Forget() })
	const pool = "p"
	a, b, c := Seat{Key: "a", Known: true}, Seat{Key: "b", Known: true}, Seat{Key: "c", Known: true}
	spent := func(s Seat) Seat { s.Used = 100; return s }
	Apply(Drain, pool, []Seat{spent(a), b, c}) // on b
	if got := Apply(Drain, pool, []Seat{spent(a), spent(b), spent(c)}); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("all used up: %v, want it kept on b", got)
	}
	// b turned off: the first not used up, c
	if got := Apply(Drain, pool, []Seat{spent(a), c}); !slices.Equal(got, []int{1, 0}) {
		t.Fatalf("b gone: %v, want c first", got)
	}
	// another pool goes its own way
	if got := Apply(Drain, "other", []Seat{a, b}); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("another pool: %v", got)
	}
	// Forget starts the pool anew
	Forget(pool)
	if got := Apply(Drain, pool, []Seat{a, b, c}); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("forgotten: %v", got)
	}
}

func TestDrainIsBuiltin(t *testing.T) {
	s, ok := Builtin[provider.Drain]
	if !ok || !s.Exact {
		t.Fatalf("Builtin[%q] = %+v, %v; want an exact strategy", provider.Drain, s, ok)
	}
}

// Refused for its quota (Drained) and renewed before the pool's next
// request: the pool goes on from it all the same. Another pool on another
// seat is left where it is.
func TestDrainedGoesOnThoughItRenewed(t *testing.T) {
	Forget()
	t.Cleanup(func() { Forget() })
	a, b, c := Seat{Key: "a"}, Seat{Key: "b"}, Seat{Key: "c"}
	Apply(Drain, "p", []Seat{a, b, c})
	Apply(Drain, "q", []Seat{b, c})
	Drained("a")
	if got := Apply(Drain, "p", []Seat{a, b, c}); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("a refused, then renewed: %v, want b first", got)
	}
	if got := Apply(Drain, "p", []Seat{a, b, c}); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("asked again: %v, want b still first", got)
	}
	if got := Apply(Drain, "q", []Seat{b, c}); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("another pool: %v, want it left on b", got)
	}
}
