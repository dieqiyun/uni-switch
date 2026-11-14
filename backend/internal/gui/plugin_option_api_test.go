package gui

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A switch a plugin declares (#2038, Vocllum) is set by itself: its value
// is kept under the plugin's provider id and key, the provider's other
// options and other providers' are left as they were, a save of the
// Settings page, which doesn't send them, keeps them, and a key the host
// wouldn't list is refused.
func TestPluginOptionSetting(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("APPDATA", filepath.Join(h, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(h, "AppData", "Local"))
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	s := settings.Load()
	s.PluginOptions = map[string]map[string]bool{"other": {"x": true}, "optco": {"beta": true}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	handler := Handler(nil, nil)
	post := func(path, body string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec.Code
	}
	has := func(want map[string]map[string]bool) {
		t.Helper()
		got := settings.Load().PluginOptions
		if !maps.EqualFunc(got, want, maps.Equal) {
			t.Fatalf("pluginOptions = %v, want %v", got, want)
		}
	}
	if c := post("/api/settings/plugin-option", `{"provider":"optco","key":"auto-claim","on":false}`); c != http.StatusOK {
		t.Fatalf("set: %d", c)
	}
	has(map[string]map[string]bool{"other": {"x": true}, "optco": {"beta": true, "auto-claim": false}})
	if c := post("/api/settings", `{"theme":"dark"}`); c != http.StatusOK {
		t.Fatalf("settings: %d", c)
	}
	has(map[string]map[string]bool{"other": {"x": true}, "optco": {"beta": true, "auto-claim": false}})
	for _, bad := range []string{`{"key":"beta","on":true}`, `{"provider":"optco","key":"Bad Key","on":true}`, `{"provider":"optco","key":"","on":true}`} {
		if c := post("/api/settings/plugin-option", bad); c == http.StatusOK {
			t.Errorf("%s saved", bad)
		}
	}
	has(map[string]map[string]bool{"other": {"x": true}, "optco": {"beta": true, "auto-claim": false}})
}
