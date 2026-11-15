package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUniSwitchExtensionEndpointsRemoved(t *testing.T) {
	h := builtinHome(t)
	for _, path := range []string{
		"/api/plugins", "/api/plugins/builtin", "/api/plugins/updates", "/api/plugins/agents",
		"/api/plugins/add", "/api/plugin-signin", "/api/plugin-signin/prompt",
		"/api/plugin-pages", "/api/plugin-panels", "/api/plugin-pages/read", "/api/plugin-pages/options",
		"/api/settings/plugin-option", "/api/settings/plugin-checkin", "/api/usage/plugin-checkin",
		"/plugins.js", "/pluginpages.js", "/plugins.css", "/_uni-switch/plugin/old-token/entry.html",
	} {
		for _, method := range []string{"GET", "POST"} {
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			if r.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, r.Code)
			}
		}
	}
}
