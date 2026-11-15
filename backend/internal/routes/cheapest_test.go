package routes

import (
	"slices"
	"testing"
)

// Cheapest first puts the seat whose model costs least now first, by a
// million tokens in and out; a tie there goes to the cheaper cache read,
// costs within a millionth are a tie and keep the listed order, and a
// seat with no known price goes after every priced one, in its listed
// place. A free model (Cost 0, priced) goes first.
func TestCheapestOrdersByPriceNow(t *testing.T) {
	seats := []Seat{
		{Key: "unpriced-a"},
		{Key: "dear", Priced: true, Cost: 0.84},
		{Key: "cheap-b", Priced: true, Cost: 0.6, CacheCost: 0.02},
		{Key: "cheap-a", Priced: true, Cost: 0.6, CacheCost: 0.01},
		{Key: "tie-1", Priced: true, Cost: 0.7},
		{Key: "unpriced-b"},
		{Key: "tie-2", Priced: true, Cost: 0.7 + 1e-9},
		{Key: "free", Priced: true},
	}
	var got []string
	for _, i := range Apply(Cheapest, "g", seats) {
		got = append(got, seats[i].Key)
	}
	want := []string{"free", "cheap-a", "cheap-b", "tie-1", "tie-2", "dear", "unpriced-a", "unpriced-b"}
	if !slices.Equal(got, want) {
		t.Fatalf("Cheapest first ordered %v, want %v", got, want)
	}
	if !Cheapest.ByPrice || Builtin["cheap"].Order == nil {
		t.Fatal("Cheapest first isn't priced or registered as cheap")
	}
}
