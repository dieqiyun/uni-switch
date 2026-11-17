package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/desktopupdate"
)

func TestUniSwitchDistributionNeverInstallsAnotherProduct(t *testing.T) {
	called := false
	handler := uniSwitchPage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }), "development", nil)
	for _, path := range []string{"/api/update", "/api/update/install", "/api/update/check", "/api/uninstall", "/api/cli", "/api/settings/login"} {
		called = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if called || rec.Code != http.StatusConflict {
			t.Fatalf("%s reached foreign installer: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/providers", "/api/state", "/api/set", "/api/plugins", "/api/agents/connect/codex"} {
		called = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if !called || rec.Code != 204 {
			t.Fatalf("business handler %s was replaced", path)
		}
	}
}

func TestUniSwitchUsesDesktopUpdaterAndRefusesPageSuppliedPaths(t *testing.T) {
	u := desktopupdate.New("0.5.30", t.TempDir(), func(string) error { t.Fatal("installer must not run"); return nil })
	defer u.Close()
	h := uniSwitchPage(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("reached standalone updater") }), "0.5.30", u)
	read := httptest.NewRecorder()
	h.ServeHTTP(read, httptest.NewRequest("GET", "/api/update", nil))
	if !strings.Contains(read.Body.String(), `"state":"idle"`) || read.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("desktop updater was replaced by current-version stub: %s", read.Body.String())
	}
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/api/update/download", `{"url":"https://example.com/run.exe"}`, 400},
		{"/api/update/install", `{"path":"C:\\other.exe"}`, 400},
		{"/api/update/check", `{} {}`, 400},
		{"/api/update/install", `{"task":"unknown"}`, 409},
		{"/api/update/other", `{}`, 404},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		if rec.Code != tc.status {
			t.Errorf("%s accepted invalid request: %d", tc.path, rec.Code)
		}
	}
}

func TestUniSwitchManagementUsesCookieAndSameOrigin(t *testing.T) {
	h := webHandler("18999", "test-key", 0, uniSwitchPage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "dev", nil))
	read := httptest.NewRecorder()
	h.ServeHTTP(read, httptest.NewRequest("GET", "http://127.0.0.1:18999/?k=test-key", nil))
	cookies := read.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].HttpOnly {
		t.Fatal("missing HTTP-only startup cookie")
	}
	for _, site := range []string{"same-origin", "cross-site"} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:18999/api/set", strings.NewReader("{}"))
		req.AddCookie(cookies[0])
		req.Header.Set("Sec-Fetch-Site", site)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if site == "same-origin" && rec.Code != 204 {
			t.Fatalf("own page refused: %d", rec.Code)
		}
		if site == "cross-site" && rec.Code != 403 {
			t.Fatalf("other page accepted: %d", rec.Code)
		}
	}
}
