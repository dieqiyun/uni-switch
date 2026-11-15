package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A gateway that resells several vendors names each catalog; its models
// are looked up in them in order (#17).
func TestSeveralCatalogs(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	os.MkdirAll(filepath.Join(cache, "uni-switch"), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{
	  "openai": {"models": {"gpt-5.5": {"id":"gpt-5.5","name":"GPT-5.5","reasoning_options":[{"type":"effort","values":["low","high"]}]}}},
	  "deepseek": {"models": {"deepseek-chat": {"id":"deepseek-chat","name":"DeepSeek V4","reasoning_options":[{"type":"effort","values":["high","max"]}]}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	if err := Save(Provider{ID: "gw", Name: "Gateway", Key: "k", Chat: "https://gw.example/v1", Catalog: "OpenAI,, deepseek openai"}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("gw")
	if err != nil || p.Catalog != "openai, deepseek" {
		t.Fatalf("catalog %q %v", p.Catalog, err)
	}
	names := map[string]string{}
	for _, m := range p.Available() {
		names[m.ID] = m.Name
	}
	if names["gpt-5.5"] != "GPT-5.5" || names["deepseek-chat"] != "DeepSeek V4" {
		t.Fatalf("available %v", names)
	}
	// the vendor's own list, ids prefixed the gateway's way
	live := catalog.Decorate([]catalog.Model{{ID: "deepseek/deepseek-chat"}, {ID: "internal-x"}}, p.Available())
	if live[0].Name != "DeepSeek V4" || len(live[0].Efforts) != 2 || live[1].Efforts != nil {
		t.Fatalf("decorated %+v", live)
	}
}

// OpenCode Go serves Grok on /responses only and MiniMax on /messages;
// models.dev says so per model, and the rest are left to the endpoints.
// Another vendor's per-model word isn't taken: it often means another host.
func TestOpenCodeModelAPIs(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	os.MkdirAll(filepath.Join(cache, "uni-switch"), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{
	  "opencode-go": {"models": {
	    "grok-4.7": {"id":"grok-4.7","provider":{"npm":"@ai-sdk/openai"}},
	    "minimax-m3": {"id":"minimax-m3","provider":{"npm":"@ai-sdk/anthropic"}},
	    "glm-5.3": {"id":"glm-5.3"}}},
	  "zenmux": {"models": {"claude-x": {"id":"claude-x","provider":{"npm":"@ai-sdk/anthropic","api":"https://zenmux.ai/api/anthropic/v1"}}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	oc := Provider{ID: "opencode-go", Catalog: "opencode-go", Chat: "https://opencode.ai/zen/go/v1",
		Responses: "https://opencode.ai/zen/go/v1", Anthropic: "https://opencode.ai/zen/go"}
	for model, want := range map[string]string{"grok-4.7": "responses", "minimax-m3": "anthropic", "glm-5.3": ""} {
		got := oc.APIs(model)
		if want == "" && got != nil || want != "" && (len(got) != 1 || string(got[0]) != want) {
			t.Errorf("%s: %v, want %q", model, got, want)
		}
	}
	zm := Provider{ID: "zenmux", Catalog: "zenmux", Chat: "https://zenmux.ai/api/v1", Anthropic: "https://zenmux.ai/api/anthropic"}
	if got := zm.APIs("claude-x"); got != nil {
		t.Errorf("zenmux claude-x: %v", got)
	}
}
