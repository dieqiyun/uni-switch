package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Disconnect puts back only what uni-switch changed: a key the user edited by
// hand while Codex was on uni-switch keeps the user's value, though uni-switch had
// saved what was there before it connected (yhwangl on X).
func TestCodexDisconnectKeepsHandEdits(t *testing.T) {
	home, read := codexHome(t, "", "model = \"gpt-5.5\"\nmodel_reasoning_effort = \"low\"\nmodel_catalog_json = \"/x/old-list.json\"\n")
	theirs := filepath.Join(home, "my-list.json")
	os.WriteFile(theirs, []byte(`{"models":[]}`), 0o644)
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, "model_provider = \"uni-switch\"") {
		t.Fatalf("not uni-switch's provider:\n%s", cfg)
	}
	// by hand, with uni-switch connected: an effort and a model list of their own
	cfg = strings.Replace(cfg, "model_reasoning_effort = \"low\"", "model_reasoning_effort = \"high\"", 1)
	lines := strings.Split(cfg, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "model_catalog_json") {
			lines[i] = "model_catalog_json = " + quoteTOML(theirs)
		}
	}
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(strings.Join(lines, "\n")), 0o644)
	if err := codex(home).Disconnect(); err != nil {
		t.Fatal(err)
	}
	cfg = read()
	for _, want := range []string{"model_reasoning_effort = \"high\"\n", "model_catalog_json = " + quoteTOML(theirs) + "\n", "model = \"gpt-5.5\"\n"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("missing %q after Disconnect:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "model_provider = \"uni-switch\"") {
		t.Errorf("still on uni-switch:\n%s", cfg)
	}
}

// What uni-switch changed and the user didn't is put back as it was.
func TestCodexDisconnectPutsBackWhatUniSwitchChanged(t *testing.T) {
	home, read := codexHome(t, "", "model = \"gpt-5.5\"\nmodel_reasoning_effort = \"low\"\n")
	if err := codex(home).Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := codex(home).Disconnect(); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	for _, want := range []string{"model_reasoning_effort = \"low\"\n", "model = \"gpt-5.5\"\n"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("missing %q after Disconnect:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "model_catalog_json") || strings.Contains(cfg, "model_provider =") {
		t.Errorf("uni-switch's keys left:\n%s", cfg)
	}
}

func quoteTOML(s string) string { return "\"" + strings.ReplaceAll(s, `\`, `\\`) + "\"" }
