package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestPanelShortcutNames(t *testing.T) {
	for _, c := range []struct{ in, goos, want string }{
		{"", "darwin", ""},
		{"Cmd+Shift+m", "darwin", "Shift+Cmd+M"},
		{"Option+Ctrl+Space", "darwin", "Ctrl+Option+Space"},
		{"Super+Alt+k", "linux", "Alt+Super+K"},
		{"Cmd+K", "windows", "Super+K"},
		{"Option+1", "windows", "Alt+1"},
		{"F5", "windows", "F5"},
		{"Shift+F20", "linux", "Shift+F20"},
	} {
		got, err := panelShortcut(c.in, c.goos)
		if err != nil || got != c.want {
			t.Errorf("panelShortcut(%q, %s) = %q, %v; want %q", c.in, c.goos, got, err, c.want)
		}
	}
	// a key typing needs, a key no system's table has, a made-up modifier
	for _, in := range []string{"K", "Shift+K", "Ctrl+Tab", "Ctrl+F21", "Ctrl+", "Hyper+K", "Ctrl+KK", "Cmd+;"} {
		if got, err := panelShortcut(in, "darwin"); err == nil {
			t.Errorf("panelShortcut(%q) = %q, want refused", in, got)
		}
	}
}

func TestWaylandSession(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, c := range []struct {
		goos string
		env  map[string]string
		want bool
	}{
		{"linux", map[string]string{"XDG_SESSION_TYPE": "wayland"}, true},
		{"linux", map[string]string{"XDG_SESSION_TYPE": "x11", "WAYLAND_DISPLAY": "wayland-1"}, false},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, true},
		{"linux", map[string]string{}, false},
		{"darwin", map[string]string{"XDG_SESSION_TYPE": "wayland"}, false},
	} {
		if got := waylandSession(c.goos, env(c.env)); got != c.want {
			t.Errorf("waylandSession(%s, %v) = %v", c.goos, c.env, got)
		}
	}
	if got := shellWord("/opt/My Apps/uni-switch"); got != `'/opt/My Apps/uni-switch'` {
		t.Errorf("shellWord: %s", got)
	}
}

// The shortcut is saved only once the app has it; one the system refuses
// is not saved and the old one stays bound; the Settings page's own save
// and a sync's leave it as it was.
func TestPanelShortcutSetting(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("XDG_SESSION_TYPE", "x11")
	var bound []string
	refuse := ""
	was := onPanelShortcut
	onPanelShortcut = func(s string) error {
		if s != "" && s == refuse {
			return errPanelKeyTaken
		}
		bound = append(bound, s)
		return nil
	}
	t.Cleanup(func() { onPanelShortcut = was })
	srv := Handler(&zoomWindows{}, nil)
	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec
	}
	rec := post("/api/settings/panel-shortcut", `{"shortcut":"Ctrl+Alt+Shift+K"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	key, _ := panelShortcut("Ctrl+Alt+Shift+K", runtime.GOOS)
	var got struct {
		PanelShortcut string
		PanelKeys     panelKeysJSON
	}
	if json.Unmarshal(rec.Body.Bytes(), &got); got.PanelShortcut != key || settings.Load().PanelShortcut != key || !got.PanelKeys.Can {
		t.Fatalf("answered %+v, saved %q", got, settings.Load().PanelShortcut)
	}
	if len(bound) != 1 || bound[0] != key {
		t.Fatalf("bound %v", bound)
	}
	refuse = "Ctrl+J"
	if rec := post("/api/settings/panel-shortcut", `{"shortcut":"Ctrl+J"}`); rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), errPanelKeyTaken.Error()) || settings.Load().PanelShortcut != key {
		t.Fatalf("a refused shortcut: %d %s, saved %q", rec.Code, rec.Body, settings.Load().PanelShortcut)
	}
	if rec := post("/api/settings/panel-shortcut", `{"shortcut":"Shift+K"}`); rec.Code == http.StatusOK || settings.Load().PanelShortcut != key || len(bound) != 1 {
		t.Fatalf("a key typing needs was taken: %d", rec.Code)
	}
	if rec := post("/api/settings", `{"theme":"dark","lang":"en"}`); rec.Code != http.StatusOK || settings.Load().PanelShortcut != key {
		t.Fatalf("the Settings page's save changed the shortcut: %d %q", rec.Code, settings.Load().PanelShortcut)
	}
	if rec := post("/api/settings/panel-shortcut", `{"shortcut":""}`); rec.Code != http.StatusOK || settings.Load().PanelShortcut != "" || bound[len(bound)-1] != "" {
		t.Fatalf("clearing: %d, saved %q, bound %v", rec.Code, settings.Load().PanelShortcut, bound)
	}
	// a Wayland desktop binds it itself: nothing is taken, and the row is
	// told the command
	if runtime.GOOS != "linux" {
		return
	}
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	if rec := post("/api/settings/panel-shortcut", `{"shortcut":"Ctrl+K"}`); rec.Code == http.StatusOK || settings.Load().PanelShortcut != "" || len(bound) != 2 {
		t.Fatalf("taken on Wayland: %d, bound %v", rec.Code, bound)
	}
	if st := panelKeysState(); st.Can || !strings.HasSuffix(st.Command, " panel") {
		t.Fatalf("Wayland's state: %+v", st)
	}
}
