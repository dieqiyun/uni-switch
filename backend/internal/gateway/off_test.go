package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A session started before its provider was switched off asks for the
// model its agent was on then; the agent has since been moved to another
// (agent.Reseat, #200), and that one stands in. An agent still on it is
// told the provider is switched off, as before.
func TestProviderOffStandIn(t *testing.T) {
	fresh(t)
	a, b := &keyed{}, &keyed{}
	serveOn(t, "a", "ka", []string{"m"}, a)
	serveOn(t, "b", "kb", []string{"m"}, b)
	if err := provider.SetOff("b", true); err != nil {
		t.Fatal(err)
	}
	now := "a/m"
	StandIn = func(agent, model string) string { return now }
	t.Cleanup(func() { StandIn = nil })
	s := New()
	code, body := postAs(t, s, "", `{"model":"b/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "from ka") || len(b.tried) != 0 {
		t.Fatalf("b/m with its agent moved to a/m: %d %s, b tried %v", code, body, b.tried)
	}
	now = "b/m"
	code, body = postAs(t, s, "", `{"model":"b/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 404 || !strings.Contains(body, "switched off in uni-switch") || len(b.tried) != 0 {
		t.Fatalf("b/m with its agent still on it: %d %s, b tried %v", code, body, b.tried)
	}
}
