package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUniSwitchDistributionNeverInstallsAnotherProduct(t *testing.T) {
	called := false
	handler := uniSwitchPage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }), "development")
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

func TestUniSwitchManagementUsesCookieAndSameOrigin(t *testing.T) {
	h := webHandler("18999", "test-key", 0, uniSwitchPage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "dev"))
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
