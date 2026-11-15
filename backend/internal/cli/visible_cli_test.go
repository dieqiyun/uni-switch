package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The visibility is kept under the agent's id lowered, because VisibleTo
// looks it up lowered: a WSL id's case must not split the key the write
// makes from the key the read asks for, and the one all takes back out is
// the same key again.
func TestVisibleKeyLowered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { provider.Delete("relay") })
	if err := visibleCmd([]string{"CODEX", "relay"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().Visible["codex"]; !ok {
		t.Fatalf("kept under a key that is not the id lowered: %v", settings.Load().Visible)
	}
	if names, ok := provider.VisibleTo("CODEX"); !ok || len(names) != 1 || names[0] != "relay" {
		t.Fatalf("VisibleTo: %v, %v", names, ok)
	}
	if err := visibleCmd([]string{"CODEX", "all"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().Visible["codex"]; ok {
		t.Fatal("all took the visibility out under another key than the id lowered")
	}
}

// all shows the agent every model again: the models taken out of its list
// one by one (HiddenModels) go with the narrowing, or the recovery the
// hints point at doesn't. An agent shown none of them — only the models
// picked for it, and none picked — stayed none through --show-new (which
// keeps what it is shown by taking the rest out) and through all, and only
// deleting hiddenModels.<agent> from settings.json brought the models
// back (#4587: a DSH behind a Docker gateway saw an empty model list).
func TestVisibleAllShowsTakenOutModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2", "m3"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { provider.Delete("relay") })
	if err := provider.SetOnlyPicked("dsh", true); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetPickedModels("dsh", nil); err != nil {
		t.Fatal(err)
	}
	if shown, _ := provider.CatalogFor("dsh"); len(shown) != 0 {
		t.Fatalf("want none picked, shown none; got %d", len(shown))
	}
	if err := visibleCmd([]string{"dsh", "--show-new"}); err != nil {
		t.Fatal(err)
	}
	if ids := settings.Load().HiddenModels["dsh"]; len(ids) != 3 {
		t.Fatalf("--show-new keeps what is shown by taking the rest out; hidden: %v", ids)
	}
	if shown, _ := provider.CatalogFor("dsh"); len(shown) != 0 {
		t.Fatalf("want still none shown after --show-new; got %d", len(shown))
	}
	if err := visibleCmd([]string{"dsh", "all"}); err != nil {
		t.Fatal(err)
	}
	if ids := settings.Load().HiddenModels["dsh"]; len(ids) != 0 {
		t.Fatalf("all left %d taken out: %v", len(ids), ids)
	}
	if shown, _ := provider.CatalogFor("dsh"); len(shown) != 3 {
		t.Fatalf("all shows %d of 3: %v", len(shown), shown)
	}
}

// all takes the only-picked switch out too: an agent shown only the models
// picked for it kept the ones not picked off through all, though the error
// naming it says all shows it every model (#3432: zcode/GLM-5.3 stayed off
// Claude Code after all).
func TestVisibleAllTakesOnlyPickedOut(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2", "m3"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { provider.Delete("relay") })
	if err := provider.SetOnlyPicked("claude", true); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetPickedModels("claude", []string{"m1"}); err != nil {
		t.Fatal(err)
	}
	if err := visibleCmd([]string{"claude", "all"}); err != nil {
		t.Fatal(err)
	}
	if _, only := provider.PickedModels("claude"); only {
		t.Fatal("all left claude shown only the models picked for it")
	}
	if shown, _ := provider.CatalogFor("claude"); len(shown) != 3 {
		t.Fatalf("all shows %d of 3: %v", len(shown), shown)
	}
}

// uni-switch models and uni-switch visible take a WSL agent by its id, in the
// distro's own case or any other: before, both lowered what was typed and
// then matched it exactly against ids that keep the distro's case (#271).
func TestWSLAgentByIDInModelsAndVisible(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	root := t.TempDir()
	h := filepath.Join(root, "home", "me", ".claude")
	if err := os.MkdirAll(h, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agent.FakeWSL(map[string]string{"Ubuntu": "home:/home/me\ndir:.claude\n"}, map[string]string{"Ubuntu": root}))
	for _, q := range []string{"claude@wsl:Ubuntu", "claude@wsl:ubuntu", "CLAUDE@WSL:UBUNTU"} {
		if err := models([]string{q}); err != nil {
			t.Errorf("models %q: %v", q, err)
		}
		if err := visibleCmd([]string{q}); err != nil {
			t.Errorf("visible %q: %v", q, err)
		}
	}
}
