package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// usedReply is a chat answer of 350 tokens, which the key's limits count.
func usedReply(id string) string {
	return `{"id":"` + id + `","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":300,"completion_tokens":50}}`
}

// keyCall sends body to path with secret as the gateway key.
func keyCall(h http.Handler, secret, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// spentSaid fails t unless w is the 429 of a spent limit of some models:
// it names the key and the models, says when it resets, and leaves the
// key's reset header out, as the key's other models are still served.
func spentSaid(t *testing.T, w *httptest.ResponseRecorder, scope string) {
	t.Helper()
	var e struct {
		Error struct{ Message, Type string }
	}
	json.Unmarshal(w.Body.Bytes(), &e)
	if w.Code != http.StatusTooManyRequests || e.Error.Type != "rate_limit_error" || !strings.Contains(e.Error.Message, `"Capped"`) || !strings.Contains(e.Error.Message, scope) {
		t.Fatalf("refusal: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" || w.Header().Get("X-Should-Retry") != "false" {
		t.Fatalf("headers %v", w.Header())
	}
}

// A request that moves to another model midway — a provider's fallback
// when it is out of quota — doesn't move onto a model a spent limit of
// the key's counts: that one is never asked, and the refusal says why.
func TestKeyModelLimitHoldsAFallback(t *testing.T) {
	fresh(t)
	budget.Forget()
	pinLimitClock(t)
	plan := &fake{t: t, ctype: "application/json", reply: usedReply("from-plan")}
	spare := &fake{t: t, ctype: "application/json", reply: usedReply("from-spare")}
	twoProviders(t, plan, spare)
	keys, secrets := newCaller(t, "Capped")
	setLimit(t, keys[0].ID, &access.Limit{Period: "day", Tokens: 350, Models: []string{"spare/*"}})
	h := New().Handler()
	if w := keyCall(h, secrets[0], "/v1/chat/completions", `{"model":"spare/m2","messages":[{"role":"user","content":"hi"}]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	plan.code, plan.reply = 429, `{"error":{"message":"slow down"}}`
	w := keyCall(h, secrets[0], "/v1/chat/completions", chatReq)
	if spare.calls != 1 || strings.Contains(w.Body.String(), "from-spare") {
		t.Fatal("the fallback went to a spent model", spare.calls, w.Code, w.Body.String())
	}
	if plan.calls == 0 {
		t.Fatal("the model asked wasn't tried")
	}
}

// System One's decision model is held by a spent limit that counts it.
func TestKeyModelLimitHoldsSystemOne(t *testing.T) {
	fresh(t)
	budget.Forget()
	pinLimitClock(t)
	var decided int
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decided++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"answers":{"intent":{"choice":"bug"}},"usage":{"input_tokens":300,"output_tokens":50}}`))
	}))
	t.Cleanup(jev.Close)
	if err := provider.Save(provider.Provider{ID: "jev", Name: "Jev", Key: "k", Decide: jev.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Capped")
	setLimit(t, keys[0].ID, &access.Limit{Period: "day", Tokens: 300, Models: []string{"jev/*"}})
	h := New().Handler()
	const ask = `{"model":"jev/jev-latest","state":{"message":"hi"},"questions":{"intent":{"type":"choice"}}}`
	if w := keyCall(h, secrets[0], "/v1/systemone", ask); w.Code != 200 || decided != 1 {
		t.Fatal("within the limit", w.Code, w.Body.String())
	}
	spentSaid(t, keyCall(h, secrets[0], "/v1/systemone", ask), "jev/*")
	if decided != 1 {
		t.Fatal("a spent decider was asked", decided)
	}
}

// A limit of the whole key, as every one from before, is untouched by
// limits of some models beside it: it counts every model, and says its
// reset to a remote uni-switch.
func TestKeyModelLimitBesideTheWholeKey(t *testing.T) {
	fresh(t)
	budget.Forget()
	pinLimitClock(t)
	plan := &fake{t: t, ctype: "application/json", reply: usedReply("from-plan")}
	spare := &fake{t: t, ctype: "application/json", reply: usedReply("from-spare")}
	twoProviders(t, plan, spare)
	keys, secrets := newCaller(t, "Capped")
	if _, err := access.Update("limit-key", access.Change{Key: keys[0].ID, Limit: &access.Limit{Period: "day", Tokens: 1050}}); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("limit-key", access.Change{Key: keys[0].ID, Index: new(1), Limit: &access.Limit{Period: "day", Tokens: 10000, Models: []string{"plan/*"}}}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	for _, model := range []string{"spare/m2", "spare/m2", "plan/m1"} {
		if w := keyCall(h, secrets[0], "/v1/chat/completions", `{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`); w.Code != 200 {
			t.Fatal(model, w.Code, w.Body.String())
		}
	}
	w := keyCall(h, secrets[0], "/v1/chat/completions", `{"model":"spare/m2","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 429 {
		t.Fatal("the whole key's limit", w.Code, w.Header(), w.Body.String())
	}
}
