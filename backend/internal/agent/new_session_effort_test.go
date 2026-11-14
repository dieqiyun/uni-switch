package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// effortModels is a provider whose models take the levels models.dev gives
// them: lo-hi-max (low, high, max) and all-five (low to max).
func effortModels(t *testing.T) {
	t.Helper()
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{
	  "lo-hi-max":{"id":"lo-hi-max","reasoning_options":[{"type":"effort","values":["low","high","max"]}]},
	  "all-five":{"id":"all-five","reasoning_options":[{"type":"effort","values":["low","medium","high","xhigh","max"]}]}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "volc", Name: "Volc", Key: "k", Chat: "http://127.0.0.1:1/api/v3",
		Models: []string{"lo-hi-max", "all-five"}}); err != nil {
		t.Fatal(err)
	}
}

// The effort new sessions start with lasts a model switch (Creat.wu on
// Discord: set to the highest, a session started there, and dropped to
// Default once a routing group was picked in Codex). Codex starts a model
// picked in /model or its app at the catalog's default_reasoning_level:
// that is the chosen level where the model takes it, its nearest otherwise
// (a tie goes up), and the middle one only when the effort is Default.
func TestCodexNewSessionEffortLastsModelSwitch(t *testing.T) {
	home, read := codexHome(t, "", "model_reasoning_effort = \"xhigh\"\n")
	effortModels(t)
	cx := codex(home)
	defaults := func() map[string]string {
		t.Helper()
		b, _ := os.ReadFile(filepath.Join(home, ".codex", "uni-switch-models.json"))
		var cat struct {
			Models []struct {
				Slug    string  `json:"slug"`
				Default *string `json:"default_reasoning_level"`
			} `json:"models"`
		}
		if err := json.Unmarshal(b, &cat); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, m := range cat.Models {
			if m.Default != nil {
				out[m.Slug] = *m.Default
			}
		}
		return out
	}
	if err := cx.Fields[0].Set("volc/all-five"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); cx.Fields[1].Get() != "xhigh" {
		t.Fatalf("the user's xhigh changed on a model that takes it:\n%s", cfg)
	}
	d := defaults()
	// the model switched to in Codex starts at the chosen level, or the
	// nearest it takes: xhigh sits between high and max, and goes up
	if d["volc/all-five"] != "xhigh" || d["volc/lo-hi-max"] != "max" {
		t.Fatalf("catalog defaults for xhigh: %v", d)
	}
	// a new choice is in the list at once, and in the tag of the list the
	// gateway hands a signed-in Codex, so it asks for it again
	tag := provider.CodexListTag()
	if err := cx.Fields[1].Set("low"); err != nil {
		t.Fatal(err)
	}
	if provider.CodexListTag() == tag {
		t.Fatal("the list's tag stayed the same for another effort")
	}
	if d := defaults(); d["volc/all-five"] != "low" || d["volc/lo-hi-max"] != "low" {
		t.Fatalf("catalog defaults for low: %v", d)
	}
	// Default keeps the middle of the road
	if err := cx.Fields[1].Set(""); err != nil {
		t.Fatal(err)
	}
	if d := defaults(); d["volc/all-five"] != "medium" || d["volc/lo-hi-max"] != "high" {
		t.Fatalf("catalog defaults for Default: %v", d)
	}
	// picked in uni-switch, a model without the level settles on the nearest
	if err := cx.Fields[1].Set("xhigh"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Fields[0].Set("volc/lo-hi-max"); err != nil {
		t.Fatal(err)
	}
	if v := cx.Fields[1].Get(); v != "max" {
		t.Fatalf("xhigh on lo-hi-max settled on %q", v)
	}
}

// An effort picked in the Codex app keeps the app running. The app saves
// it as model_reasoning_effort, the catalog's default_reasoning_level
// follows it (above), and the next sync rewrote the catalog. The route the
// app's app-server is ended on (codex_app_server.go) hashed the catalog's
// bytes, so each pick ended it a few minutes later, and the ChatGPT app
// read that as a crash and relaunched (indekkusu141209 on X, #4550). The
// route is where requests go, never the list: the app reads a new list at
// its next launch.
func TestCodexEffortPickKeepsTheAppRunning(t *testing.T) {
	home, read := codexHome(t, "", "model_reasoning_effort = \"xhigh\"\n")
	effortModels(t)
	cx := codex(home)
	if err := cx.Fields[0].Set("volc/all-five"); err != nil {
		t.Fatal(err)
	}
	cat := filepath.Join(home, ".codex", "uni-switch-models.json")
	listed, _ := os.ReadFile(cat)
	before, _, known := codexAppRoute()
	if !known {
		t.Fatal("route not known")
	}
	// as the Codex app saves a pick of its own
	if err := edit.SetTOMLTop(filepath.Join(home, ".codex", "config.toml"), edit.KV{Path: "model_reasoning_effort", Value: "low"}); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	if now, _ := os.ReadFile(cat); string(now) == string(listed) {
		t.Fatalf("the effort didn't reach the catalog, so this tests nothing:\n%s", read())
	}
	if r, _, _ := codexAppRoute(); r != before {
		t.Fatalf("an effort picked in the Codex app changed its route: its app-server is ended for it\n%s", read())
	}
}

// Grok Build: a model picked without the chosen level starts at its
// nearest, not at Grok's default.
func TestGrokNewSessionEffortLastsModelSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	effortModels(t)
	path := filepath.Join(home, "grok", "config.toml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("[models]\ndefault = \"grok-4.6\"\ndefault_reasoning_effort = \"xhigh\"\n"), 0o644)
	a := grok(home)
	if err := a.Field("model").Set("uni-switch/volc/all-five"); err != nil {
		t.Fatal(err)
	}
	if v := a.Field("effort").Get(); v != "xhigh" {
		t.Fatalf("xhigh on a model that takes it: %q", v)
	}
	if err := a.Field("model").Set("uni-switch/volc/lo-hi-max"); err != nil {
		t.Fatal(err)
	}
	if v := a.Field("effort").Get(); v != "max" {
		t.Fatalf("xhigh on lo-hi-max: %q", v)
	}
}

// Claude Code: Opus 5.5 reads only its own modelSettings level, and a
// routing group or another vendor's model picked later in Claude Code reads
// the top one: the chosen level is written there too, so it starts at it.
func TestClaudeNewSessionEffortLastsModelSwitch(t *testing.T) {
	home, path := claudeSettings(t, `{"model": "claude-opus-5-5"}`)
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	a := claude(home)
	if err := a.Field("effort").Set("xhigh"); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(path, "modelSettings.claude-opus-5-5.effortLevel"); v != "xhigh" {
		t.Fatalf("opus 5.5's own:\n%s", readFile(path))
	}
	// another vendor's model picked: it reads the top level
	if err := a.Field("model").Set("deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if v := a.Field("effort").Get(); v != "xhigh" {
		t.Fatalf("after a switch to another vendor's model: %q\n%s", v, readFile(path))
	}
}
