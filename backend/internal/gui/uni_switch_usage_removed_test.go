package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUniSwitchUsageReportsRemoved(t *testing.T) {
	h := builtinHome(t)
	for _, path := range []string{"/api/usage", "/api/usage/requests", "/api/usage/requests.csv", "/api/usage/heatmap", "/api/sessions/stats", "/api/sessions/overview", "/api/context", "/api/tune", "/api/tune/apply", "/api/tune/undo", "/plugins/usage/client.js", "/plugins/usage/page.js"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, w.Code)
			}
		}
	}
}
