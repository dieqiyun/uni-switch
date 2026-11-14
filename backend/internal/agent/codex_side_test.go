package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// SideModel is the model of uni-switch's Codex is on beside its ChatGPT
// sign-in, which the gateway answers the Codex app's titles, descriptions
// and summaries on (atom_Ray on X): none on one of Codex's own models, and
// none with uni-switch Codex's provider, where those requests reach uni-switch's
// own API on the model Codex asks for.
func TestCodexSideModel(t *testing.T) {
	home, _ := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "model = \"gpt-a\"\n")
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"models":[{"slug":"gpt-a","display_name":"A","priority":1}]}`), 0o644)
	cx := codex(home)
	if got := cx.SideModel(); got != "" {
		t.Fatalf("not connected: %q", got)
	}
	if ok, err := cx.Join(); err != nil || !ok {
		t.Fatalf("join: %v %v", ok, err)
	}
	if got := cx.SideModel(); got != "" {
		t.Fatalf("joined on Codex's own model: %q", got)
	}
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if got := cx.SideModel(); got != "fake/m1" {
		t.Fatalf("a uni-switch model beside the sign-in: %q", got)
	}
	if err := cx.Field("login").Set("api"); err != nil {
		t.Fatal(err)
	}
	if got := cx.SideModel(); got != "" {
		t.Fatalf("uni-switch Codex's provider: %q", got)
	}
}

// CodexSideSet says whether Codex's config names the model for its
// memories itself, the gateway then leaving those calls as Codex sent
// them; a config it can't read counts as naming one. Its subagents' model
// isn't asked: the gateway leaves a subagent on the model it was named.
func TestCodexSideSet(t *testing.T) {
	home, _ := codexHome(t, `{}`, "model = \"gpt-a\"\n")
	if CodexSideSet("memories") {
		t.Fatal("nothing set")
	}
	config := filepath.Join(home, ".codex", "config.toml")
	os.WriteFile(config, []byte("[agents]\ndefault_subagent_model = \"gpt-b\"\n[memories]\nextract_model = \"gpt-c\"\n"), 0o644)
	if !CodexSideSet("memories") {
		t.Fatal("set")
	}
	os.WriteFile(config, []byte("[agents]\ndefault_subagent_model = \"gpt-b\"\n"), 0o644)
	if CodexSideSet("memories") {
		t.Fatal("the subagents' alone")
	}
	os.WriteFile(config, []byte("[agents\n"), 0o644)
	if !CodexSideSet("memories") {
		t.Fatal("an unreadable config counts as set")
	}
}
