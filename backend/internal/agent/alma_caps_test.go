package agent

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Alma is told which of uni-switch's models reason, in each model's
// capabilityOverrides: it works a model's capabilities out from models.dev
// by its id, and finds nothing for uni-switch's (codex/gpt-5.5), so Alma
// offered no thinking for every Codex model (#730). The levels it is given
// are those of the model's that Alma knows, in its order; a model that
// doesn't reason says nothing of it; an override the user set in Alma is
// kept; and a provider already told is left alone.
func TestAlmaToldReasoning(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "key",
		Models: []string{"gpt-5.5", "switch-only"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "gpt-5.5", Efforts: []string{"xhigh", "minimal", "low", "medium", "high", "ultra"}, Context: 400000, Output: 128000, Images: true},
		{ID: "switch-only", Reasoning: true},
	}); err != nil {
		t.Fatal(err)
	}
	f := startAlma(t)
	a := alma()
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "uni-switch/think/gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	var mine map[string]any
	for _, p := range f.providers {
		if p["name"] == "uni-switch" {
			mine = p
		}
	}
	if mine == nil {
		t.Fatalf("no uni-switch provider: %v", f.providers)
	}
	caps := func(id string) map[string]any {
		t.Helper()
		av, _ := mine["availableModels"].([]any)
		for _, m := range av {
			if o := m.(map[string]any); o["id"] == id {
				c, _ := o["capabilityOverrides"].(map[string]any)
				return c
			}
		}
		t.Fatalf("%s not in %v", id, av)
		return nil
	}
	js := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if got := js(caps("think/gpt-5.5")); got != `{"contextWindow":400000,"functionCalling":true,"maxOutputTokens":128000,"reasoning":true,"reasoningLevels":["low","medium","high","xhigh"],"vision":true}` {
		t.Fatalf("gpt-5.5: %s", got)
	}
	if c := caps("think/switch-only"); c["reasoning"] != true || c["reasoningLevels"] != nil {
		t.Fatalf("switch-only: %v", c)
	}
	if c := caps("relay/glm-4.6"); c["reasoning"] != nil || c["reasoningLevels"] != nil || js(c["contextWindow"]) != "204800" {
		t.Fatalf("glm-4.6: %v", c)
	}
	f.takeWrites()

	// told already: nothing written
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if w := f.takeWrites(); len(w) != 0 {
		t.Fatalf("sync wrote %v", w)
	}

	// as an older uni-switch left it, with an override of the user's: told
	// again, the user's kept
	for _, m := range mine["availableModels"].([]any) {
		o := m.(map[string]any)
		switch o["id"] {
		case "think/gpt-5.5":
			o["capabilityOverrides"] = map[string]any{"functionCalling": false}
		case "relay/glm-4.6":
			o["capabilityOverrides"] = map[string]any{"pricing": map[string]any{"input": 1.0}}
		default:
			delete(o, "capabilityOverrides")
		}
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if w := f.takeWrites(); len(w) != 1 || w[0] != "PUT /api/providers/"+mine["id"].(string)+"/models" {
		t.Fatalf("sync wrote %v", w)
	}
	if got := js(caps("think/gpt-5.5")); got != `{"contextWindow":400000,"functionCalling":false,"maxOutputTokens":128000,"reasoning":true,"reasoningLevels":["low","medium","high","xhigh"],"vision":true}` {
		t.Fatalf("gpt-5.5 synced: %s", got)
	}
	if c := caps("think/switch-only"); c["reasoning"] != true || c["reasoningLevels"] != nil {
		t.Fatalf("switch-only synced: %v", c)
	}
	if c := caps("relay/glm-4.6"); c["reasoning"] != nil || js(c["contextWindow"]) != "204800" || js(c["pricing"]) != `{"input":1}` {
		t.Fatalf("glm-4.6 synced: %v", c)
	}
}

// Alma is told what each of uni-switch's models costs, at the price uni-switch's
// own usage pages count it at: Alma looks a model's price up by its id on
// models.dev and found none for uni-switch's, so its usage page counted every
// call through uni-switch at $0, "unpriced" (#919). A model uni-switch has no price
// for says nothing of it.
func TestAlmaToldPricing(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "key",
		Models: []string{"priced", "unpriced"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelPrice("think/priced", &catalog.Price{Input: 2, Output: 8, CacheRead: 0.5, CacheWrite: 2.5}); err != nil {
		t.Fatal(err)
	}
	f := startAlma(t)
	a := alma()
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "uni-switch/think/priced"); err != nil {
		t.Fatal(err)
	}
	var mine map[string]any
	for _, p := range f.providers {
		if p["name"] == "uni-switch" {
			mine = p
		}
	}
	pricing := func(id string) any {
		t.Helper()
		av, _ := mine["availableModels"].([]any)
		for _, m := range av {
			if o := m.(map[string]any); o["id"] == id {
				c, _ := o["capabilityOverrides"].(map[string]any)
				return c["pricing"]
			}
		}
		t.Fatalf("%s not in %v", id, av)
		return nil
	}
	if b, _ := json.Marshal(pricing("think/priced")); string(b) != `{"cacheRead":0.5,"cacheWrite":2.5,"input":2,"output":8}` {
		t.Fatalf("priced: %s", b)
	}
	if p := pricing("think/unpriced"); p != nil {
		t.Fatalf("unpriced: %v", p)
	}
}

// Alma is told every one of uni-switch's chat models calls tools, as the
// gateway's /v1/models says (toolsTold): Alma looks function calling up on
// models.dev by the model's id, found nothing for a routing group's name or
// a plugin's model, and put the tools in the prompt (hermesToolMiddleware),
// so DeepSeek wrote its DSML call text out, shown raw (ethanchi on
// Discord). A Tool Calling the user set in Alma, on or off, is kept, and
// isn't written over again at every sync; one taken away is told again.
func TestAlmaToldTools(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "relay2", Name: "Relay Two", Chat: "https://example.test/v1", Key: "key",
		Models: []string{"DeepSeek", "kimi"}}); err != nil {
		t.Fatal(err)
	}
	f := startAlma(t)
	a := alma()
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "uni-switch/relay2/DeepSeek"); err != nil {
		t.Fatal(err)
	}
	var mine map[string]any
	for _, p := range f.providers {
		if p["name"] == "uni-switch" {
			mine = p
		}
	}
	if mine == nil {
		t.Fatalf("no uni-switch provider: %v", f.providers)
	}
	model := func(id string) map[string]any {
		t.Helper()
		for _, m := range mine["availableModels"].([]any) {
			if o := m.(map[string]any); o["id"] == id {
				return o
			}
		}
		t.Fatalf("%s not in %v", id, mine["availableModels"])
		return nil
	}
	tools := func(id string) any {
		t.Helper()
		c, _ := model(id)["capabilityOverrides"].(map[string]any)
		return c["functionCalling"]
	}
	ids, _ := mine["models"].([]any)
	if len(ids) == 0 {
		t.Fatal("no models told")
	}
	for _, id := range ids {
		if got := tools(id.(string)); got != true {
			t.Fatalf("%s: functionCalling %v, want true", id, got)
		}
	}
	f.takeWrites()

	// the user turned Tool Calling off for one, and Alma lost another's
	// overrides: the user's kept, the other told again, and once told
	// nothing is written again
	model("relay2/DeepSeek")["capabilityOverrides"].(map[string]any)["functionCalling"] = false
	delete(model("relay2/kimi"), "capabilityOverrides")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if w := f.takeWrites(); len(w) != 1 {
		t.Fatalf("sync wrote %v", w)
	}
	if got := tools("relay2/DeepSeek"); got != false {
		t.Fatalf("user's off: functionCalling %v", got)
	}
	if got := tools("relay2/kimi"); got != true {
		t.Fatalf("kimi told again: functionCalling %v", got)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if w := f.takeWrites(); len(w) != 0 {
		t.Fatalf("second sync wrote %v", w)
	}
}
