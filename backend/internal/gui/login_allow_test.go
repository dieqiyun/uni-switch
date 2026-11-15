package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/autostart"
)

// Open at login switched off in System Settings, its launch agent still
// there, reads as needing to be allowed, not as on (#2120, Anoninz): the
// row says so and opens Login Items, the one System Settings pane the page
// may open.
func TestLoginNeedsAllowing(t *testing.T) {
	sandboxHome(t)
	prev := loginState
	t.Cleanup(func() { loginState = prev })
	for _, c := range []struct {
		st           autostart.State
		login, allow bool
	}{{autostart.Off, false, false}, {autostart.On, true, false}, {autostart.NeedsAllowing, true, true}} {
		loginState = func() autostart.State { return c.st }
		s := settingsState()
		if s.Login != c.login || s.LoginAllow != c.allow {
			t.Errorf("%s: login %v, loginAllow %v; want %v, %v", c.st, s.Login, s.LoginAllow, c.login, c.allow)
		}
		b, _ := json.Marshal(s)
		if got := strings.Contains(string(b), `"loginAllow":true`); got != c.allow {
			t.Errorf("%s: loginAllow in the JSON is %v: %s", c.st, got, b)
		}
	}

	w := &importWindows{}
	mux := http.NewServeMux()
	providerRoutes(mux, w)
	for _, link := range []string{autostart.LoginItemsURL, "x-apple.systempreferences:com.apple.preference.security?Privacy_FilesAndFolders", "x-apple.systempreferences:"} {
		w.opened = ""
		body, _ := json.Marshal(map[string]string{"url": link})
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/open", strings.NewReader(string(body))))
		if want := link == autostart.LoginItemsURL; (w.opened == link) != want {
			t.Errorf("%s: opened %q", link, w.opened)
		}
	}
}
