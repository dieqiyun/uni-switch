package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// hits counts the requests each upstream took, and fails them all as out
// of credit: a failure that passes the request on, with no retry.
type hits struct {
	sync.Mutex
	n map[string]int
}

func (h *hits) on(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Lock()
		h.n[id]++
		h.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		io.WriteString(w, `{"error":{"message":"insufficient balance"}}`)
	})
}

func (h *hits) got() map[string]int {
	h.Lock()
	defer h.Unlock()
	out := map[string]int{}
	for k, v := range h.n {
		out[k] = v
	}
	return out
}

// probeOne keeps every ready candidate, and of the resting ones only the
// one back soonest, at its place; with one resting or none, all.
func TestProbeOne(t *testing.T) {
	fresh(t)
	c := func(id string) candidate { return candidate{p: provider.Provider{ID: id}, model: "m", rest: id} }
	cs := []candidate{c("a"), c("b"), c("c"), c("d"), c("e")}
	ids := func(cs []candidate) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.p.ID)
		}
		return strings.Join(out, " ")
	}
	if got := ids(probeOne(cs)); got != "a b c d e" {
		t.Fatalf("none resting: %s", got)
	}
	now := time.Now()
	restingUntil.Lock()
	restingUntil.m["b"] = now.Add(4 * time.Minute)
	restingUntil.Unlock()
	if got := ids(probeOne(cs)); got != "a b c d e" {
		t.Fatalf("one resting: %s", got)
	}
	restingUntil.Lock()
	restingUntil.m["d"] = now.Add(2 * time.Minute)
	restingUntil.m["e"] = now.Add(2 * time.Minute)
	restingUntil.Unlock()
	if got := ids(probeOne(cs)); got != "a c d" {
		t.Fatalf("three resting: %s", got)
	}
	if got := ids(probeOne(cs[1:2])); got != "b" {
		t.Fatalf("lone: %s", got)
	}
}
