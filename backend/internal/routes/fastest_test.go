package routes

import (
	"slices"
	"testing"
)

// Speed first: the seat that answered fastest lately goes first, those
// within a tenth of each other keep their listed order, one resting or
// used up goes behind the awake whatever its speed, and a seat that told
// no pace is weighed at the middle pace of those that did.
func TestFastestOrdersBySpeedLately(t *testing.T) {
	cases := []struct {
		name  string
		seats []Seat
		want  []int
	}{
		{
			name:  "the fastest first token first",
			seats: []Seat{{Key: "a", TTFT: 2000}, {Key: "b", TTFT: 600}, {Key: "c", TTFT: 1200}},
			want:  []int{1, 2, 0},
		},
		{
			// a: 500 + 200 tok at 20/s = 10.5s; b: 900 + 200 at 100/s = 2.9s
			name:  "a quick first token that writes slowly loses",
			seats: []Seat{{Key: "a", TTFT: 500, TPS: 20}, {Key: "b", TTFT: 900, TPS: 100}},
			want:  []int{1, 0},
		},
		{
			// within a tenth: listed order, so they don't trade on every request
			name:  "alike keep their order",
			seats: []Seat{{Key: "a", TTFT: 1050}, {Key: "b", TTFT: 1000}, {Key: "c", TTFT: 3000}},
			want:  []int{0, 1, 2},
		},
		{
			name:  "resting goes behind the awake, used up behind those",
			seats: []Seat{{Key: "a", TTFT: 100, Resting: true}, {Key: "b", TTFT: 100, Used: 99}, {Key: "c", TTFT: 5000}},
			want:  []int{2, 0, 1},
		},
		{
			// b told no pace: it counts at the middle pace of a, c and d
			// (100), so 1000+2000 = 3000, behind a's 2500+200 and ahead
			// of c's 3000+2000; at its first token alone it would lead
			name: "a seat without a pace counts at the middle pace",
			seats: []Seat{{Key: "a", TTFT: 2500, TPS: 1000}, {Key: "b", TTFT: 1000},
				{Key: "c", TTFT: 3000, TPS: 100}, {Key: "d", TTFT: 3000, TPS: 50}},
			want: []int{0, 1, 2, 3},
		},
		{
			name:  "none measured: their order",
			seats: []Seat{{Key: "a"}, {Key: "b"}, {Key: "c"}},
			want:  []int{0, 1, 2},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Fastest.Order("order:"+c.name, c.seats)
			if !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// One not measured lately isn't kept behind the measured for good: one
// request in ProbeEvery puts it first, so its answer measures it, and the
// turn goes round those not measured.
func TestFastestGivesTheUnmeasuredATurn(t *testing.T) {
	seats := []Seat{{Key: "a", TTFT: 500}, {Key: "b"}, {Key: "c"}}
	firsts := map[int]int{}
	var probesAt []int
	for n := 1; n <= 4*ProbeEvery; n++ {
		got := Fastest.Order("probe-pool", seats)
		if len(got) != 3 {
			t.Fatalf("request %d: %v", n, got)
		}
		firsts[got[0]]++
		if got[0] != 0 {
			probesAt = append(probesAt, n)
		}
	}
	if want := []int{ProbeEvery, 2 * ProbeEvery, 3 * ProbeEvery, 4 * ProbeEvery}; !slices.Equal(probesAt, want) {
		t.Fatalf("the unmeasured went first at requests %v, want %v", probesAt, want)
	}
	if firsts[1] != 2 || firsts[2] != 2 {
		t.Fatalf("the probe's turn didn't go round b and c: %v", firsts)
	}
	// another pool counts its own requests
	if got := Fastest.Order("probe-other", seats); got[0] != 0 {
		t.Fatalf("a fresh pool probed on its first request: %v", got)
	}
	// a resting seat never gets a probe's turn
	rest := []Seat{{Key: "a", TTFT: 500}, {Key: "b", Resting: true}}
	for n := 0; n < 2*ProbeEvery; n++ {
		if got := Fastest.Order("probe-rest", rest); got[0] != 0 {
			t.Fatalf("request %d put the resting seat first: %v", n, got)
		}
	}
}

func TestFastestIsBuiltinFast(t *testing.T) {
	st, ok := Builtin["fast"]
	if !ok || !st.BySpeed || st.Exact {
		t.Fatalf("fast: %+v %v", st, ok)
	}
}
