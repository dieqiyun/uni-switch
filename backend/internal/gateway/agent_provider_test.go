package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestClientProviderSelectionBlocksOtherProviderAndFallback(t *testing.T) {
	plan := &fake{t: t, ctype: "application/json", code: 402, reply: `{"error":{"message":"Insufficient Balance"}}`}
	spare := &fake{t: t, ctype: "application/json", reply: `{"id":"from-spare","choices":[]}`}
	twoProviders(t, plan, spare)
	if err := provider.SetAgentProvider("codex", "plan"); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, model := range []string{"plan/m1", "m1", "spare/m2"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer uni-switch-codex")
		s.Handler().ServeHTTP(rec, req)
		if rec.Code < 400 {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body)
		}
		if spare.calls != 0 {
			t.Fatalf("%s spent another provider", model)
		}
		if model == "spare/m2" && rec.Code != 403 {
			t.Fatalf("expected explicit provider refusal: %d %s", rec.Code, rec.Body)
		}
	}
}
