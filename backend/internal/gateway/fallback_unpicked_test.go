package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider's fallback the user unchecked in its own provider's list is
// skipped as a group's member is (#3911): the fallback editor says "No
// provider serves it now; it is skipped", and the gateway did send there.
func TestFallbackSkipsModelRemovedFromProviderPicks(t *testing.T) {
	fresh(t)
	plan, a, b := &keyed{fail: map[string]int{"kp": 429}}, &keyed{}, &keyed{}
	serveOn(t, "a", "ka", []string{"x", "y"}, a)
	serveOn(t, "b", "kb", []string{"x"}, b)
	up := httptest.NewServer(plan)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Key: "kp", Models: []string{"m"}, Chat: up.URL + "/v1", Fallback: []string{"a/x", "b/x"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModels("a", []string{"y"}); err != nil {
		t.Fatal(err)
	}
	code, body := postAs(t, New(), "", `{"model":"plan/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "from kb") || len(a.tried) != 0 || len(plan.tried) != 1 {
		t.Fatalf("plan/m, its fallback a/x unchecked: %d %s, plan tried %v, a tried %v, b tried %v", code, body, plan.tried, a.tried, b.tried)
	}
}
