package gui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetiredGroupAPIsUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	h := Handler(nil, nil)
	for _, path := range []string{"/api/groups", "/api/groups/save", "/api/groups/delete", "/api/groups/pick", "/api/groups/skip", "/api/groups/templates", "/api/groups/template"} {
		for _, method := range []string{"GET", "POST"} {
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader("{}")))
			if r.Code != 404 {
				t.Errorf("%s %s: %d", method, path, r.Code)
			}
		}
	}
}
