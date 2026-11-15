package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// Codex's own models are read from the cache under CODEX_HOME when it is
// set, as Codex CLI writes it there, not from ~/.codex.
func TestCodexReadsCacheUnderCodexHome(t *testing.T) {
	home, codexHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", codexHome)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[{"slug":"in-home","visibility":"list"}]}`), 0o644)
	os.WriteFile(filepath.Join(codexHome, "models_cache.json"), []byte(`{"models":[{"slug":"in-codex-home","visibility":"list"}]}`), 0o644)
	if ms := Codex(); len(ms) != 1 || ms[0].ID != "in-codex-home" {
		t.Errorf("Codex() = %+v, want in-codex-home alone", ms)
	}
}
