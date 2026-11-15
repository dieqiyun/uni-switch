package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// pagePlugins writes plugins.json with the example page plugin
// (testdata/page-plugin), on, and a copy of it switched off; it answers
// their specs.
func pagePlugins(t *testing.T) (on, off string) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(h, ".local", "share"))
	t.Setenv("UNI_SWITCH_BUN", "")
	t.Setenv("PATH", "")
	copyDir := func(to string) {
		t.Helper()
		err := filepath.WalkDir("testdata/page-plugin", func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel("testdata/page-plugin", p)
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
	on, off = filepath.Join(h, "src", "glance"), filepath.Join(h, "src", "glance-off")
	copyDir(on)
	copyDir(off)
	// a file of the plugin's own, but out of its page's folder, and one out
	// of the plugin, which a link in the page's folder points to
	if err := os.WriteFile(filepath.Join(h, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h, "secret.txt"), filepath.Join(on, "page", "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"plugins": []map[string]any{{"spec": on}, {"spec": off, "off": true}}})
	if err := os.WriteFile(filepath.Join(settings.Dir(), "plugins.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return on, off
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
