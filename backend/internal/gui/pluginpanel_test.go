package gui

import (
	"encoding/json"

	"os"
	"path/filepath"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// copyTree copies a testdata folder to to.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// panelPlugins writes plugins.json with the example panel plugin
// (testdata/panel-plugin), on and a copy switched off, and two copies of
// the example page plugin that each ask to replace Usage; it answers
// their specs in that order.
func panelPlugins(t *testing.T) (on, off, first, second string) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(h, ".local", "share"))
	t.Setenv("UNI_SWITCH_BUN", "")
	t.Setenv("PATH", "")
	on, off = filepath.Join(h, "src", "notes"), filepath.Join(h, "src", "notes-off")
	first, second = filepath.Join(h, "src", "usage-a"), filepath.Join(h, "src", "usage-b")
	copyTree(t, "testdata/panel-plugin", on)
	copyTree(t, "testdata/panel-plugin", off)
	for i, dir := range []string{first, second} {
		copyTree(t, "testdata/page-plugin", dir)
		pkg := `{"name":"usage-` + string(rune('a'+i)) + `","version":"0.1.0","uni-switch":{"page":{"title":"Usage ` + string(rune('A'+i)) + `","file":"page/index.html","replaces":"usage"},"permissions":["usage:summary"]}}`
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(h, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"plugins": []map[string]any{{"spec": on}, {"spec": off, "off": true}, {"spec": first}, {"spec": second}}})
	if err := os.WriteFile(filepath.Join(settings.Dir(), "plugins.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return on, off, first, second
}
