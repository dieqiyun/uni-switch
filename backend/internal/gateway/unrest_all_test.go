package gateway

import (
	"slices"
	"testing"
	"time"
)

// A key whose value was replaced is asked again for every model (#1869):
// its own rest and each model's (key/model) are lifted, another key's,
// and a provider's whose id only starts the same, stay.
func TestUnrestAllLiftsAKeysModels(t *testing.T) {
	fresh(t)
	until := time.Now().Add(time.Hour)
	restingUntil.Lock()
	for _, k := range []string{"relay#aaa", "relay#aaa/m1", "relay#aaa/m2", "relay#bbb", "relay#bbb/m1", "relay#aaab", "relay"} {
		restingUntil.m[k] = until
	}
	restingUntil.Unlock()
	if n := New().UnrestAll("relay#aaa"); n != 3 {
		t.Fatalf("lifted %d, want 3", n)
	}
	restingUntil.Lock()
	var left []string
	for k := range restingUntil.m {
		left = append(left, k)
	}
	restingUntil.Unlock()
	slices.Sort(left)
	if want := []string{"relay", "relay#aaab", "relay#bbb", "relay#bbb/m1"}; !slices.Equal(left, want) {
		t.Fatalf("left %v, want %v", left, want)
	}
}
