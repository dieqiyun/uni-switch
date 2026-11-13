package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Cursor Private Inference reads uni-switch's /models for each model's APIs
// (api_types: it asks Anthropic Messages only when no OpenAI one is
// listed) and its limits (capabilities.context_length) (#299): asked with
// its key, each model has them, the ones its provider serves it on; asked
// by any other client, the list is as it was.
func TestCursorLocalModels(t *testing.T) {
	setup(t, provider.Anthropic, &fake{})
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DS", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"flash", "claude-sonnet-4-6"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("ds", "http://127.0.0.1:1/v1", []catalog.Model{{ID: "flash", Context: 128000, Output: 8192}, {ID: "claude-sonnet-4-6"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "or", Name: "OR", Key: "k", Chat: "http://127.0.0.1:2/v1", Anthropic: "http://127.0.0.1:2", Models: []string{"claude-opus-4-7"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("or", "http://127.0.0.1:2/v1", []catalog.Model{{ID: "claude-opus-4-7", Efforts: []string{"low", "medium", "high"}}}); err != nil {
		t.Fatal(err)
	}
	list := func(key string, ua ...string) map[string]map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		for _, v := range ua {
			req.Header.Set("User-Agent", v)
		}
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		var out struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(rec.Body.String())
		}
		got := map[string]map[string]any{}
		for _, d := range out.Data {
			got[d["id"].(string)] = d
		}
		return got
	}
	types := func(m map[string]any) []string {
		var out []string
		for _, v := range m["api_types"].([]any) {
			out = append(out, v.(string))
		}
		return out
	}

	got := list(TokenFor("cursor-local"))
	f, ds := got["fake/m1"], got["ds/flash"]
	if f == nil || ds == nil {
		t.Fatalf("models: %v", got)
	}
	if ts := types(f); !slices.Equal(ts, []string{"anthropic_messages"}) {
		t.Errorf("an Anthropic model's api_types = %v", ts)
	}
	if ts := types(ds); !slices.Equal(ts, []string{"chat_completions"}) {
		t.Errorf("a Chat model's api_types = %v", ts)
	}
	c, _ := ds["capabilities"].(map[string]any)
	if c["context_length"] != float64(128000) || c["max_output_tokens"] != float64(8192) {
		t.Errorf("capabilities = %v", ds["capabilities"])
	}
	// its picker drops capabilities that don't say the model streams, calls
	// tools and writes text, and with them its Reasoning control (mamba on
	// Discord); one that thinks says so, with its levels
	if c["supports_tool_use"] != true || c["supports_streaming"] != true || c["supports_reasoning"] != false {
		t.Errorf("capabilities = %v", c)
	}
	if mods, _ := c["output_modalities"].([]any); len(mods) != 1 || mods[0] != "text" {
		t.Errorf("output_modalities = %v", c["output_modalities"])
	}
	cl := got["or/claude-opus-4-7"]
	if cl == nil {
		t.Fatalf("models: %v", got)
	}
	cc, _ := cl["capabilities"].(map[string]any)
	if efforts, _ := cc["reasoning_effort"].([]any); cc["supports_reasoning"] != true || len(efforts) != 3 {
		t.Errorf("a thinking model's capabilities = %v", cc)
	}
	// it sends a Claude's level on Anthropic Messages only, so a Claude
	// served on both is listed there alone; a Chat-only one stays on Chat
	if ts := types(cl); !slices.Equal(ts, []string{"anthropic_messages"}) {
		t.Errorf("a Claude on Chat and Messages: api_types = %v", ts)
	}
	if ts := types(got["ds/claude-sonnet-4-6"]); !slices.Equal(ts, []string{"chat_completions"}) {
		t.Errorf("a Claude on Chat alone: api_types = %v", ts)
	}

	for _, key := range []string{Token, TokenFor("cursor"), TokenFor("opencode")} {
		for id, m := range list(key) {
			if m["api_types"] != nil || m["capabilities"] != nil {
				t.Errorf("%s: %s has %v %v", key, id, m["api_types"], m["capabilities"])
			}
		}
	}

	// the models picked on its row are its picker's: one taken out isn't
	// listed with its key, and still is for another agent's
	if err := provider.SetHiddenModels("cursor-local", []string{"ds/flash"}); err != nil {
		t.Fatal(err)
	}
	if got := list(TokenFor("cursor-local")); got["ds/flash"] != nil || got["fake/m1"] == nil {
		t.Errorf("with ds/flash taken out: %v", got)
	}
	if got := list(TokenFor("opencode")); got["ds/flash"] == nil {
		t.Errorf("taken out of another agent's list: %v", got)
	}

	// asked with another key of uni-switch's, typed in its Open configuration,
	// it still says who it is in its User-Agent and is shown its own list,
	// with the fields its Reasoning control needs (mamba on Discord)
	for _, ua := range []string{"Cursor-CLI/unknown", "Cursor/3.23.12"} {
		got := list(Token, ua)
		if got["ds/flash"] != nil || got["fake/m1"] == nil {
			t.Errorf("%s with key %s: %v", ua, Token, got)
		}
		if c, _ := got["or/claude-opus-4-7"]["capabilities"].(map[string]any); c["supports_reasoning"] != true {
			t.Errorf("%s with key %s: capabilities = %v", ua, Token, c)
		}
	}
}
