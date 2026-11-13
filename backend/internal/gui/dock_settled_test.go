//go:build !nogui

package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A save from the Settings page keeps how the file's Dock choice is read,
// which the page never sends: a new install's With window stays With
// window (in the Dock while its window is open), and an older file's Hide
// stays Hide.
func TestSettingsSaveKeepsDockReading(t *testing.T) {
	page := `{"theme":"dark","dock":false,"dockWindow":false,"dockNever":%s}`
	for _, c := range []struct {
		name, file, never string
		inDock            bool
	}{
		{"a new install", "", "false", true},
		{"an older file with no choice", `{}`, "true", false},
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if c.file != "" {
			if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settings.Path(), []byte(c.file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := settings.Load(); inDock(got, true, false) != c.inDock {
			t.Fatalf("%s: in the Dock with its window open read as %v", c.name, !c.inDock)
		}
		// what the page posts is what it was shown
		rec := httptest.NewRecorder()
		Handler(nil, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings", strings.NewReader(strings.Replace(page, "%s", c.never, 1))))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
		got := settings.Load()
		if got.Theme != "dark" || inDock(got, true, false) != c.inDock {
			t.Fatalf("%s: after a save from the page, theme %q, in the Dock with its window open %v", c.name, got.Theme, inDock(got, true, false))
		}
	}
}
