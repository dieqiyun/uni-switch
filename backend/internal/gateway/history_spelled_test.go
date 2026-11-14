package gateway

import "testing"

// A route kept with a try marked swapped for its model spelled with other
// separators (Volcengine Ark: deepseek-v4.1-flash answered as
// deepseek-v4-1-flash) is read as not swapped; another model stays swapped.
func TestRouteKeptSpelledOtherwiseNotSwapped(t *testing.T) {
	r := &Route{Swapped: true, Served: "deepseek-v4-1-flash", Tries: []Try{{Model: "deepseek-v4.1-flash", Served: "deepseek-v4-1-flash", Swapped: true}}}
	r.routedAgain()
	if r.Swapped || r.Tries[0].Swapped {
		t.Fatalf("still swapped: %+v", r)
	}
	r = &Route{Swapped: true, Served: "deepseek-v4-flash", Tries: []Try{{Model: "deepseek-v4.1-flash", Served: "deepseek-v4-flash", Swapped: true}}}
	r.routedAgain()
	if !r.Swapped || !r.Tries[0].Swapped {
		t.Fatalf("a real swap was cleared: %+v", r)
	}
}
