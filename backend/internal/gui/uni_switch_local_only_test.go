package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalOnlyRemovedSettingsAndBrowserModes(t *testing.T) {
	sandboxHome(t)
	h := Handler(nil, nil)
	for _, path := range []string{"/api/settings/lan", "/api/settings/allowips"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"on":true}`)))
		if w.Code != http.StatusNotFound {
			t.Errorf("removed endpoint %s: %d %s", path, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/web-page", strings.NewReader(`{"mode":"lan"}`)))
	if w.Code == http.StatusOK {
		t.Fatal("browser page accepted LAN mode")
	}
	if err := os.MkdirAll(filepath.Dir(appPageFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appPageFile(), []byte(`{"mode":"lan","key":"0123456789abcdef0123456789abcdef"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if addr, _ := AppPage(); addr != "" {
		t.Fatalf("old LAN configuration still starts a page: %s", addr)
	}
}

func TestLocalOnlyWebCannotBindNetwork(t *testing.T) {
	for _, addr := range []string{":0", "0.0.0.0:0", "[::]:0", "192.168.1.1:0"} {
		if web, err := StartWeb(addr, "local-only-test"); err == nil {
			web.srv.Close()
			t.Fatalf("management page accepted non-local listener %s", addr)
		}
	}
}
