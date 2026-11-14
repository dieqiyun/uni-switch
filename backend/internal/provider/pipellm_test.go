package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// PipeLLM's own Chat and Responses take OpenAI's family alone and its
// Messages Anthropic's; its Chat converter takes every family. Each model
// is asked where its list's type_target says it is served: GPT on
// Responses, Claude on Messages, Gemini (and any model the list doesn't
// place) on the converter.
func TestPipeLLMRoutesByTypeTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`{"object":"list","data":[
		  {"id":"gpt-5","display_name":"Gpt 5","type_target":"openai"},
		  {"id":"deepseek-v4","type_target":"openai"},
		  {"id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","type_target":"anthropic"},
		  {"id":"gemini-3-flash-preview","type_target":"gemini"},
		  {"id":"new-model"}]}`))
	}))
	defer srv.Close()
	ms, _, err := catalog.FetchAt(context.Background(), srv.URL+"/v1", "k", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("pipellm", "", ms); err != nil {
		t.Fatal(err)
	}
	p, err := FromPreset("pipellm")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != "https://api.pipellm.ai/openai/v1" || p.Responses != "https://api.pipellm.ai/v1" || p.Anthropic != "https://api.pipellm.ai" {
		t.Fatalf("endpoints %q %q %q", p.Chat, p.Responses, p.Anthropic)
	}
	for model, want := range map[string]Protocol{
		"gpt-5":                  Responses,
		"deepseek-v4":            Chat,
		"claude-sonnet-4-6":      Anthropic,
		"gemini-3-flash-preview": Chat,
		"new-model":              Chat,
	} {
		if got := p.Native(model); got != want {
			t.Errorf("%s: native %q, want %q", model, got, want)
		}
	}
	for model, never := range map[string]Protocol{"gpt-5": Anthropic, "gemini-3-flash-preview": Responses, "claude-sonnet-4-6": Chat} {
		for _, a := range p.APIs(model) {
			if a == never {
				t.Errorf("%s offered on %s", model, never)
			}
		}
	}
	for _, a := range p.APIs("gemini-3-flash-preview") {
		if a != Chat {
			t.Errorf("gemini on %s", a)
		}
	}
}
