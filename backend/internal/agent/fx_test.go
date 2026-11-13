package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestFx(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".fx", "settings.json")
	read := func() map[string]any {
		var m map[string]any
		b, _ := os.ReadFile(path)
		json.Unmarshal(b, &m)
		return m
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"theme":"dark","model":"anthropic/claude-sonnet-5","providers":{"local":{"protocol":"openai-chat-completions","base_url":"http://localhost:11434/v1","auth":{"type":"none"}}}}`), 0o644)

	a := fx(home)
	f := a.Field("model")
	if f.Get() != "anthropic/claude-sonnet-5" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	s := read()
	ps := s["providers"].(map[string]any)
	m, ok := ps["uni-switch"].(map[string]any)
	if !ok || ps["local"] == nil || s["provider"] != "uni-switch" || s["models"].(map[string]any)["uni-switch"] != "deepseek/flash" || s["theme"] != "dark" {
		t.Fatalf("settings: %v", s)
	}
	if m["protocol"] != "openai-chat-completions" || m["base_url"] != gatewayV1() || m["auth"].(map[string]any)["type"] != "none" {
		t.Fatalf("provider: %v", m)
	}
	if ms := m["model_metadata"].(map[string]any); ms["deepseek/pro"] == nil || ms["deepseek/flash"] == nil {
		t.Fatalf("models: %v", ms)
	}
	if f.Get() != "uni-switch/deepseek/flash" || a.Check() != "" {
		t.Fatalf("get: %q, check: %q", f.Get(), a.Check())
	}

	// its own model: uni-switch steps out, and the gateway's model is set
	if err := f.Set("openai/gpt-6-sol"); err != nil {
		t.Fatal(err)
	}
	s = read()
	if s["provider"] != nil || s["providers"].(map[string]any)["uni-switch"] != nil || s["providers"].(map[string]any)["local"] == nil ||
		s["models"].(map[string]any)["gateway"] != "openai/gpt-6-sol" || f.Get() != "openai/gpt-6-sol" {
		t.Fatalf("own: %v", s)
	}

	// one of the user's own providers keeps its model
	os.WriteFile(path, []byte(`{"provider":"local","models":{"local":"qwen"}}`), 0o644)
	if f.Get() != "qwen" {
		t.Fatalf("local: %q", f.Get())
	}

	// through uni-switch, then reset
	f.Set("uni-switch/deepseek/pro")
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	s = read()
	if s["provider"] != nil || s["providers"].(map[string]any)["uni-switch"] != nil || s["models"].(map[string]any)["uni-switch"] != nil {
		t.Fatalf("reset: %v", s)
	}
}

// fx is also the JSON viewer's name: a command of that name on PATH is no
// sign of the agent, its folder is.
func TestFxDetected(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	testenv.Program(t, filepath.Join(bin, "fx"), "#!/bin/sh\n")
	if fx(home).Detected() {
		t.Error("an fx command alone is taken for the agent")
	}
	if err := os.WriteFile(filepath.Join(home, ".fx"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if fx(home).Detected() {
		t.Error("a file named .fx is taken for the agent's folder")
	}
	os.Remove(filepath.Join(home, ".fx"))
	if err := os.MkdirAll(filepath.Join(home, ".fx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !fx(home).Detected() {
		t.Error("~/.fx is not taken for the agent")
	}
}

// What the user set on uni-switch's provider in fx's settings stays through
// every sync and pick, as T3 Code's did after #2030: the reviewer model fx's
// auto permission mode asks, and a tool_choice_mode turned to omit. The
// address, auth and model list stay uni-switch's; an auth the user changed goes
// back whole, as fx refuses an env beside type none.
func TestFxKeepsTheUsersSettings(t *testing.T) {
	home := syncHome(t)
	path := filepath.Join(home, ".fx", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"theme":"dark","providers":{"local":{"protocol":"openai-chat-completions","base_url":"http://localhost:11434/v1","auth":{"type":"none"}}}}`), 0o644)
	a := fx(home)
	f := a.Field("model")
	if err := f.Set("uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path,
		edit.KV{Path: "providers.uni-switch.reviewer_model", Value: "relay/glm-4.6"},
		edit.KV{Path: "providers.uni-switch.tool_choice_mode", Value: "omit"},
		edit.KV{Path: "providers.uni-switch.auth", Value: map[string]any{"type": "bearer", "env": "MY_KEY"}},
		edit.KV{Path: "providers.uni-switch.base_url", Value: "http://127.0.0.1:1/v1"},
		edit.KV{Path: "providers.uni-switch.model_metadata", Value: map[string]any{"gone/old": map[string]any{}}},
	); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string) {
		t.Helper()
		var s struct {
			Theme     string `json:"theme"`
			Providers map[string]struct {
				ReviewerModel  string         `json:"reviewer_model"`
				ToolChoiceMode string         `json:"tool_choice_mode"`
				BaseURL        string         `json:"base_url"`
				Auth           map[string]any `json:"auth"`
				ModelMetadata  map[string]any `json:"model_metadata"`
			} `json:"providers"`
		}
		raw := readFile(path)
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatalf("%v\n%s", err, raw)
		}
		p := s.Providers[uniSwitchID]
		if p.ReviewerModel != "relay/glm-4.6" || p.ToolChoiceMode != "omit" {
			t.Errorf("%s: the user's settings not kept:\n%s", when, raw)
		}
		if p.BaseURL == "http://127.0.0.1:1/v1" || p.BaseURL == "" || len(p.Auth) != 1 || p.Auth["type"] != "none" || p.ModelMetadata["gone/old"] != nil || p.ModelMetadata["deepseek/pro"] == nil {
			t.Errorf("%s: uni-switch's own parts not written:\n%s", when, raw)
		}
		if s.Theme != "dark" || s.Providers["local"].BaseURL == "" {
			t.Errorf("%s: the user's other settings changed:\n%s", when, raw)
		}
	}
	check("sync")
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	check("pick")
}
