package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Every model uni-switch lists in ZCode has native web search (原生联网搜索) on,
// so ZCode offers it its WebSearch tool (Gearclickist on Discord: each model
// had to be ticked by hand). A model the user unticked in ZCode, which
// takes the key out of uni-switch's rule, stays unticked through syncs; a value
// in the rule stays; a manual rule is the user's; and on a format other
// than Anthropic Messages, where ZCode can't send the search, uni-switch's on
// goes while the user's own value stays.
func TestZCodeWebSearch(t *testing.T) {
	home := syncHome(t)
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k",
		Models: []string{"pro", "flash", "lite", "mine"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".zcode", "v2")
	rules := filepath.Join(dir, "provider_config.json")
	os.MkdirAll(dir, 0o755)
	// a rule an older uni-switch wrote, without the key, and one the user set
	// by hand in ZCode
	os.WriteFile(rules, []byte(`{"schemaVersion":1,"config":{
	  "providerConfigRules":{"providerRules":[{"providerId":"uni-switch","providerName":"uni-switch","enabled":true,"config":{"group":"standard-personal",
	    "access":{"type":"api-key","apiKey":"uni-switch"},"api":{"type":"anthropic-messages","baseUrl":"http://127.0.0.1:3425"},
	    "personalModelIds":["deepseek/pro"],"modelOrder":["deepseek/pro"]}}]},
	  "modelConfigRules":{"providerModelRules":[{"providerId":"uni-switch","modelId":"deepseek/pro","config":{"properties":{"contextWindow":128000,"inputFormat":{"supportsImage":false}}}}],
	    "manualProviderModelRules":[{"providerId":"uni-switch","modelId":"deepseek/mine","config":{"enabled":true,"properties":{"contextWindow":64000,"supportsNativeWebSearch":false,"inputFormat":{"supportsImage":false}}}}]}}}`), 0o600)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"provider":{"uni-switch":{"name":"uni-switch","kind":"anthropic","enabled":true,"models":{}}}}`), 0o600)

	doc := func() map[string]any {
		b, _ := os.ReadFile(rules)
		d := map[string]any{}
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
		return d
	}
	mcr := func(d map[string]any) map[string]any {
		return d["config"].(map[string]any)["modelConfigRules"].(map[string]any)
	}
	props := func(d map[string]any, id string) map[string]any {
		for _, r := range mcr(d)["providerModelRules"].([]any) {
			if r := r.(map[string]any); r["providerId"] == "uni-switch" && r["modelId"] == "deepseek/"+id {
				return r["config"].(map[string]any)["properties"].(map[string]any)
			}
		}
		return nil
	}
	search := func() map[string]any {
		d, out := doc(), map[string]any{}
		for _, id := range []string{"pro", "flash", "lite", "mine"} {
			if p := props(d, id); p != nil {
				out[id] = p["supportsNativeWebSearch"]
			} else {
				out[id] = "no rule"
			}
		}
		return out
	}
	// what ZCode's model settings write: the rule changed, after uni-switch's
	// write
	edit := func(id string, change func(p map[string]any)) {
		d := doc()
		change(props(d, id))
		b, _ := json.Marshal(d)
		os.WriteFile(rules, b, 0o600)
		later := time.Now().Add(time.Minute)
		os.Chtimes(rules, later, later)
	}
	want := func(step string, w map[string]any) {
		t.Helper()
		got := search()
		for id, v := range w {
			if got[id] != v {
				t.Fatalf("%s: %s is %v, want %v (all %v)", step, id, got[id], v, got)
			}
		}
	}
	a := zcode(home)
	sync := func() {
		t.Helper()
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}

	// an older uni-switch's rule and new models get it; the manual one is left
	sync()
	want("first sync", map[string]any{"pro": true, "flash": true, "lite": true, "mine": "no rule"})
	d := doc()
	manual := mcr(d)["manualProviderModelRules"].([]any)
	if len(manual) != 1 || manual[0].(map[string]any)["config"].(map[string]any)["properties"].(map[string]any)["supportsNativeWebSearch"] != false {
		t.Fatalf("manual rule changed: %v", manual)
	}

	// unticked in ZCode: its value equals ZCode's default, so the key goes
	edit("flash", func(p map[string]any) { delete(p, "supportsNativeWebSearch") })
	// set off outright
	edit("lite", func(p map[string]any) { p["supportsNativeWebSearch"] = false })
	for range 2 {
		sync()
		want("unticked", map[string]any{"pro": true, "flash": nil, "lite": false})
	}

	// ticked again
	edit("flash", func(p map[string]any) { p["supportsNativeWebSearch"] = true })
	sync()
	want("ticked again", map[string]any{"pro": true, "flash": true, "lite": false})

	// on Chat Completions ZCode can't send the search: uni-switch's on goes,
	// the user's off stays
	d = doc()
	for _, r := range d["config"].(map[string]any)["providerConfigRules"].(map[string]any)["providerRules"].([]any) {
		if r := r.(map[string]any); r["providerId"] == "uni-switch" {
			r["config"].(map[string]any)["api"].(map[string]any)["type"] = "openai-chat-completions"
		}
	}
	b, _ := json.Marshal(d)
	os.WriteFile(rules, b, 0o600)
	later := time.Now().Add(2 * time.Minute)
	os.Chtimes(rules, later, later)
	sync()
	want("chat completions", map[string]any{"pro": nil, "flash": nil, "lite": false})

	// back on Anthropic Messages, it is on again
	d = doc()
	for _, r := range d["config"].(map[string]any)["providerConfigRules"].(map[string]any)["providerRules"].([]any) {
		if r := r.(map[string]any); r["providerId"] == "uni-switch" {
			r["config"].(map[string]any)["api"].(map[string]any)["type"] = "anthropic-messages"
		}
	}
	b, _ = json.Marshal(d)
	os.WriteFile(rules, b, 0o600)
	later = time.Now().Add(3 * time.Minute)
	os.Chtimes(rules, later, later)
	sync()
	want("anthropic again", map[string]any{"pro": true, "flash": true, "lite": false})

	// a stash that can't be read is unknown: nothing is turned on, and
	// what the rules say stays
	edit("flash", func(p map[string]any) { delete(p, "supportsNativeWebSearch") })
	os.WriteFile(stashPath(), []byte("{not json"), 0o600)
	sync()
	want("unreadable stash", map[string]any{"pro": true, "flash": nil, "lite": false})
}
