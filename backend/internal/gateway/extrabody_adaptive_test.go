package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The provider's Extra body is merged after a budget is turned into
// adaptive thinking, so a budget it put in went to claude-opus-5-5 as it
// was written, and Anthropic answers a budget with 400 there. It is fitted
// to the model like an agent's.
func TestExtraBodyBudgetFittedToAdaptiveModel(t *testing.T) {
	fresh(t)
	up := &alwaysAdaptiveVendor{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	withExtraBody(t, "anth", `{"thinking":{"type":"enabled","budget_tokens":2048}}`)
	if code, body := post(t, "/v1/messages", `{"model":"anth/claude-opus-5-5","max_tokens":32000,"stream":true,"messages":[{"role":"user","content":"hi"}]}`); code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	up.mu.Lock()
	n := len(up.bodies)
	up.mu.Unlock()
	if n != 1 {
		t.Errorf("sent %d times, want 1", n)
	}
	got := up.last()
	if th, _ := json.Marshal(got["thinking"]); string(th) != `{"display":"summarized","type":"adaptive"}` {
		t.Errorf("thinking = %s", th)
	}
	if oc, _ := got["output_config"].(map[string]any); oc["effort"] == nil {
		t.Errorf("output_config = %v, want the budget's effort", got["output_config"])
	}
}
