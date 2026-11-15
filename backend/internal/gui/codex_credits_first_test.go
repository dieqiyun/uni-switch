package gui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// jojonas_xyz on X: Use credits' "Before moving to another account" posts
// first, which sets the account's mode to first; last then takes its
// place, and Use credits alone, turned on again, keeps whichever was set.
func TestCodexCreditsFirstPosted(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	srv := Handler(nil, nil)
	post := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings/codex-credits", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	const me = "Me@example.com"
	for _, c := range []struct{ body, want string }{
		{`{"user":"Me@example.com","on":true,"first":true}`, provider.CreditsFirst},
		{`{"user":"Me@example.com","on":true}`, provider.CreditsFirst},
		{`{"user":"Me@example.com","on":true,"last":true}`, provider.CreditsLast},
		{`{"user":"Me@example.com","on":true,"first":true}`, provider.CreditsFirst},
		{`{"user":"Me@example.com","on":true,"first":false}`, provider.CreditsOn},
		{`{"user":"Me@example.com","on":true,"first":true}`, provider.CreditsFirst},
		{`{"user":"Me@example.com","on":false}`, provider.CreditsOff},
		{`{"user":"Me@example.com","on":true}`, provider.CreditsOn},
	} {
		post(c.body)
		if got := provider.CodexCreditsMode(me); got != c.want {
			t.Fatalf("after %s: %q, want %q", c.body, got, c.want)
		}
	}
}
