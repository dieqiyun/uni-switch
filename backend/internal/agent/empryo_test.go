package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEmpryo(t *testing.T) {
	home, _ := museHome(t)
	path := filepath.Join(home, ".empryo", "config.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	// a user's own: a provider of theirs, their model and a theme
	orig := `{
  "providers": [{"id": "lmstudio", "name": "LM Studio", "baseURL": "http://localhost:1234/v1"}],
  "defaultModel": "anthropic/claude-sonnet-5-5",
  "theme": "dark"
}
`
	os.WriteFile(path, []byte(orig), 0o644)
	read := func() map[string]any {
		t.Helper()
		var m map[string]any
		b, _ := os.ReadFile(path)
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return m
	}
	var want map[string]any
	json.Unmarshal([]byte(orig), &want)

	a := empryo(home)
	if a.Path != path {
		t.Fatalf("path: %s", a.Path)
	}
	f := a.Field("model")
	if f.Get() != "anthropic/claude-sonnet-5-5" || a.Check() != "" {
		t.Fatalf("own: %q %q", f.Get(), a.Check())
	}
	var vals []string
	for _, o := range f.Options(a.Values()) {
		vals = append(vals, o.Value)
	}
	if !strings.Contains(strings.Join(vals, " "), "anthropic/claude-sonnet-5-5 uni-switch/deepseek/pro") {
		t.Fatalf("options: %v", vals)
	}

	for range 2 { // twice: one uni-switch provider, and the user's model kept
		if err := f.Set("uni-switch/deepseek/pro"); err != nil {
			t.Fatal(err)
		}
	}
	m := read()
	ps, _ := m["providers"].([]any)
	if len(ps) != 2 || !reflect.DeepEqual(ps[0], want["providers"].([]any)[0]) ||
		!reflect.DeepEqual(ps[1], map[string]any{"id": "uni-switch", "name": "uni-switch", "baseURL": gatewayV1(), "modelsAPI": gatewayV1() + "/models"}) {
		t.Fatalf("providers: %v", m["providers"])
	}
	if m["defaultModel"] != "uni-switch/deepseek/pro" || m["theme"] != "dark" {
		t.Fatalf("set: %v", m)
	}
	if f.Get() != "uni-switch/deepseek/pro" || a.Check() != "" {
		t.Fatalf("on: %q %q", f.Get(), a.Check())
	}

	// uni-switch's provider pointed elsewhere
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), gatewayV1(), "http://elsewhere/v1", 1)), 0o644)
	if !strings.Contains(a.Check(), "http://elsewhere/v1") {
		t.Fatalf("check: %q", a.Check())
	}
	os.WriteFile(path, b, 0o644)

	// stepping out puts back what the user had
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if m := read(); !reflect.DeepEqual(m, want) {
		t.Fatalf("restore: %v", m)
	}

	// a file uni-switch made goes again
	os.Remove(path)
	if err := f.Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if m := read(); m["defaultModel"] != "uni-switch/deepseek/flash" {
		t.Fatalf("new: %v", m)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		b, _ := os.ReadFile(path)
		t.Fatalf("made file left: %s", b)
	}
}
