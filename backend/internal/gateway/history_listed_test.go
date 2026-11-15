package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// A route kept with a try marked swapped for the name its provider's list
// gives the model (Qoder's qfmodel answered as Qwen3.8-Flash, jzing.draw
// on Discord, 10-27) is read as not swapped; another model stays swapped.
func TestRouteKeptListedNameNotSwapped(t *testing.T) {
	t.Cleanup(usage.ListedForTest(map[string]map[string]string{"qoder": {"qfmodel": "Qwen3.8-Flash"}}))
	r := &Route{Swapped: true, Served: "Qwen3.8-Flash", Tries: []Try{{ID: "qoder", Model: "qfmodel", Served: "Qwen3.8-Flash", Swapped: true}}}
	r.routedAgain()
	if r.Swapped || r.Tries[0].Swapped {
		t.Fatalf("still swapped: %+v", r)
	}
	r = &Route{Swapped: true, Served: "Qwen3.7-Flash", Tries: []Try{{ID: "qoder", Model: "qfmodel", Served: "Qwen3.7-Flash", Swapped: true}}}
	r.routedAgain()
	if !r.Swapped || !r.Tries[0].Swapped {
		t.Fatalf("a real swap was cleared: %+v", r)
	}
}
