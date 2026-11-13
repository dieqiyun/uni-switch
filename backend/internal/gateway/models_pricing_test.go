package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// cleary5779 on Discord: an agent built on uni-switch reads /v1/models and
// shows what a call costs. Each model uni-switch knows a price of carries it
// as OpenRouter's list does, USD a token as decimal strings, at the price
// the Usage page counts it at; a model no price is known for has no
// pricing at all, never a 0.
func TestModelsListPricing(t *testing.T) {
	fresh(t)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"anthropic":{"id":"anthropic","models":{
		"claude-sonnet-4-5":{"id":"claude-sonnet-4-5","cost":{"input":3,"output":15,"cache_read":0.3,"cache_write":3.75}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"claude-sonnet-4-5", "mystery-9"}, Anthropic: "http://127.0.0.1:9/v1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.EffectivePrice("a", "mystery-9"); ok {
		t.Fatal("the fixture's unpriced model has a price; pick one no catalog prices")
	}
	get := func(path string) []byte {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:5000"
		w := httptest.NewRecorder()
		New().Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var list struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(get("/v1/models"), &list); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, m := range list.Data {
		var id string
		json.Unmarshal(m["id"], &id)
		byID[id] = m
	}
	const want = `{"completion":"0.000015","input_cache_read":"0.0000003","input_cache_write":"0.00000375","prompt":"0.000003"}`
	priced, ok := byID["a/claude-sonnet-4-5"]
	if !ok {
		t.Fatalf("a/claude-sonnet-4-5 isn't listed: %v", byID)
	}
	if got := string(priced["pricing"]); got != want {
		t.Errorf("a/claude-sonnet-4-5's pricing is %s, want %s", got, want)
	}
	if _, ok := priced["uni_switch_price"]; ok {
		t.Error("uni_switch_price told to a list that didn't ask for it")
	}
	unpriced, ok := byID["a/mystery-9"]
	if !ok {
		t.Fatalf("a/mystery-9 isn't listed: %v", byID)
	}
	if p, ok := unpriced["pricing"]; ok {
		t.Errorf("a model no price is known for has pricing %s", p)
	}

	// the model asked for alone says the same
	var one map[string]json.RawMessage
	json.Unmarshal(get("/v1/models/a/claude-sonnet-4-5"), &one)
	if got := string(one["pricing"]); got != want {
		t.Errorf("GET /v1/models/a/claude-sonnet-4-5's pricing is %s, want %s", got, want)
	}
	one = nil
	json.Unmarshal(get("/v1/models/a/mystery-9"), &one)
	if len(one) == 0 {
		t.Fatal("GET /v1/models/a/mystery-9 answered no model")
	}
	if p, ok := one["pricing"]; ok {
		t.Errorf("GET /v1/models/a/mystery-9 has pricing %s", p)
	}
}

// A price a million tokens is written a token by moving the decimal
// point: no float's rounding, no exponent.
func TestPerToken(t *testing.T) {
	for in, want := range map[float64]string{0: "0", 3: "0.000003", 0.3: "0.0000003", 3.75: "0.00000375",
		15: "0.000015", 1234567: "1.234567", 12345678.9: "12.3456789", 0.075: "0.000000075"} {
		if got := catalog.PerToken(in); got != want {
			t.Errorf("PerToken(%v) = %s, want %s", in, got, want)
		}
	}
	// a user's free price is known: both parts there, at 0
	got, _ := json.Marshal(catalog.OpenRouterPricing(catalog.Price{}))
	if string(got) != `{"completion":"0","prompt":"0"}` {
		t.Errorf("a free model's pricing: %s", got)
	}
}
