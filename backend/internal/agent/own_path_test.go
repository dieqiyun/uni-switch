package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// Every agent's own models say which way they go, as Claude Code's do
// (EZN7L2C3, #834: Codex 下拉选项中没有 via uni-switch; agy 和 grok 没有同步):
// Codex's go to OpenAI directly until it is routed through uni-switch, then
// via uni-switch; Grok Build lists the models it last listed itself, straight
// to xAI, and its own tables' to their endpoint; Antigravity CLI's own
// custom models go to their provider; DeepSeek Harness's go to DeepSeek.
func TestOwnModelsSayTheirPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	t.Setenv("DSH_HOME", "")

	// Codex
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(`{"models":[{"slug":"gpt-a","display_name":"A","priority":1}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`), 0o600)
	cfg := filepath.Join(dir, "config.toml")
	ownOf := func(a *Agent, group string) []Option {
		var out []Option
		for _, o := range a.Field("model").Options(a.Values()) {
			if o.Group == group {
				out = append(out, o)
			}
		}
		return out
	}
	own := ownOf(codex(home), "OpenAI")
	if len(own) == 0 {
		t.Fatal("Codex lists none of its own")
	}
	for _, o := range own {
		if o.Direct != "OpenAI" || o.Via {
			t.Errorf("not routed, %s goes straight to OpenAI: %+v", o.Value, o)
		}
	}
	os.WriteFile(cfg, []byte(`openai_base_url = "`+codexGatewayURL()+`"`+"\n"), 0o644)
	for _, o := range ownOf(codex(home), "OpenAI") {
		if !o.Via || o.Direct != "" {
			t.Errorf("routed, %s goes via uni-switch: %+v", o.Value, o)
		}
	}
	os.WriteFile(cfg, []byte("model_provider = \"mine\"\n[model_providers.mine]\nbase_url = \"https://relay.example/v1\"\n"), 0o644)
	for _, o := range ownOf(codex(home), "mine") {
		if o.Direct != "mine" || o.Via {
			t.Errorf("on a provider of its own, %s goes to it: %+v", o.Value, o)
		}
	}

	// Grok Build, no Grok subscription in uni-switch
	g := filepath.Join(home, "grok")
	os.MkdirAll(g, 0o755)
	os.WriteFile(filepath.Join(g, "models_cache.json"), []byte(`{"models":{
		"grok-4.7":{"info":{"id":"grok-4.7","name":"Grok 4.7","hidden":false}},
		"grok-old":{"info":{"id":"grok-old","name":"Old","hidden":true}}}}`), 0o644)
	os.WriteFile(filepath.Join(g, "config.toml"), []byte("[model.\"my-own\"]\nmodel = \"x\"\nbase_url = \"https://x.example/v1\"\n"), 0o644)
	got := map[string]Option{}
	for _, o := range ownOf(grok(home), "Grok Build") {
		got[o.Value] = o
	}
	if o := got["grok-4.7"]; o.Direct != "xAI" || o.Label != "Grok 4.7" {
		t.Errorf("Grok's own listed model: %+v", o)
	}
	if _, ok := got["grok-old"]; ok {
		t.Error("a hidden model listed")
	}
	if o := got["my-own"]; o.Direct != "x.example" {
		t.Errorf("Grok's own table: %+v", o)
	}

	// Antigravity CLI
	ag := filepath.Join(home, ".gemini", "antigravity-cli")
	os.MkdirAll(ag, 0o755)
	os.WriteFile(filepath.Join(ag, "settings.json"), []byte(`{"customModelsConfig":{"customModels":{"mine":{"apiProvider":"API_PROVIDER_ANTHROPIC","modelName":"claude-x"}}}}`), 0o644)
	if o := ownOf(agy(home), "Antigravity CLI"); len(o) != 1 || o[0].Direct != "Anthropic" {
		t.Errorf("agy's own: %+v", o)
	}

	// DeepSeek Harness 0.1.5 and after (#955): its own go to DeepSeek,
	// its llm-deepseek row left to it
	ds := filepath.Join(home, ".dsh")
	os.MkdirAll(filepath.Join(ds, "profiles", "default"), 0o755)
	os.WriteFile(filepath.Join(ds, "profiles", "default", "cordis.patch.yml"), []byte("[]\n"), 0o644)
	own = ownOf(dsh(home), "DeepSeek Harness")
	if len(own) == 0 {
		t.Fatal("dsh lists none of its own")
	}
	for _, o := range own {
		if o.Direct != "DeepSeek" || o.Via {
			t.Errorf("dsh's own %s goes straight to DeepSeek: %+v", o.Value, o)
		}
	}
}
