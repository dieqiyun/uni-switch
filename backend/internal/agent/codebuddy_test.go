package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func codebuddyHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEBUDDY_CONFIG_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	return home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return m
}

// #980: CodeBuddy Code (the China edition's CLI too) uses uni-switch's models
// through ~/.codebuddy/models.json, its session model in settings.json, and
// the user's own models and settings stay as they were.
func TestCodeBuddyCode(t *testing.T) {
	home := codebuddyHome(t)
	dir := filepath.Join(home, ".codebuddy")
	models, settings := filepath.Join(dir, "models.json"), filepath.Join(dir, "settings.json")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(models, []byte(`{"models":[{"id":"my-model","name":"Mine","vendor":"OpenAI","url":"https://example.com/v1/chat/completions","apiKey":"sk-mine"}],"availableModels":["my-model"]}`), 0o600)
	os.WriteFile(settings, []byte(`{"model":"gpt-5","language":"中文","permissions":{"allow":["Bash(ls)"]}}`), 0o600)

	var a *Agent
	for _, x := range All() {
		if x.ID == "codebuddy" {
			a = x
		}
	}
	if a == nil {
		t.Fatal("no codebuddy agent")
	}
	if !a.Detected() {
		t.Fatal("not detected")
	}
	f := a.Field("model")
	if f.Get() != "gpt-5" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, models)
	ms, _ := m["models"].([]any)
	if len(ms) != 2 || ms[0].(map[string]any)["id"] != "my-model" || ms[0].(map[string]any)["apiKey"] != "sk-mine" {
		t.Fatalf("models: %v", m)
	}
	pro := ms[1].(map[string]any)
	if pro["id"] != "deepseek/pro" || pro["vendor"] != "uni-switch" || pro["apiKey"] != "uni-switch-codebuddy" ||
		pro["url"] != gatewayV1()+"/chat/completions" || pro["supportsToolCall"] != true {
		t.Fatalf("uni-switch model: %v", pro)
	}
	if av, _ := json.Marshal(m["availableModels"]); string(av) != `["my-model","deepseek/pro"]` {
		t.Fatalf("availableModels: %s", av)
	}
	s := readJSON(t, settings)
	if s["model"] != "deepseek/pro" || s["language"] != "中文" || s["permissions"] == nil {
		t.Fatalf("settings: %v", s)
	}
	if f.Get() != "uni-switch/deepseek/pro" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if ms, _ = readJSON(t, models)["models"].([]any); len(ms) != 2 {
		t.Fatalf("after sync: %v", ms)
	}

	// back to one of CodeBuddy's own: uni-switch's models come out
	if err := f.Set("gpt-5"); err != nil {
		t.Fatal(err)
	}
	m = readJSON(t, models)
	if ms, _ = m["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != "my-model" {
		t.Fatalf("after own: %v", m)
	}
	if av, _ := json.Marshal(m["availableModels"]); string(av) != `["my-model"]` {
		t.Fatalf("availableModels: %s", av)
	}
	if s = readJSON(t, settings); s["model"] != "gpt-5" || s["language"] != "中文" {
		t.Fatalf("settings: %v", s)
	}
	// a sync leaves it off
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if ms, _ = readJSON(t, models)["models"].([]any); len(ms) != 1 {
		t.Fatalf("after sync off: %v", ms)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if s = readJSON(t, settings); s["model"] != nil || s["language"] != "中文" {
		t.Fatalf("settings after default: %v", s)
	}
}

// With no models.json, CodeBuddy Code's gets the {"models":[…]} its docs
// give; $CODEBUDDY_CONFIG_DIR is where it is.
func TestCodeBuddyCodeNewFile(t *testing.T) {
	codebuddyHome(t)
	dir := filepath.Join(t.TempDir(), "cb")
	t.Setenv("CODEBUDDY_CONFIG_DIR", dir)
	a := codebuddy(t.TempDir())
	if a.Path != filepath.Join(dir, "models.json") {
		t.Fatalf("path: %s", a.Path)
	}
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, a.Path)
	if ms, _ := m["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != "deepseek/pro" {
		t.Fatalf("models: %v", m)
	}
	if s := readJSON(t, filepath.Join(dir, "settings.json")); s["model"] != "deepseek/pro" {
		t.Fatalf("settings: %v", s)
	}
}
