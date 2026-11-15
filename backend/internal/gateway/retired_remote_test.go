package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRetiredRemoteManagementRoutesRemoved(t *testing.T) {
	fresh(t)
	h := New().Handler()
	for _, route := range []struct{ method, path string }{
		{"GET", "/v1/uni-switch/quotas/cards"},
		{"POST", "/v1/uni-switch/quotas/refresh"},
		{"POST", "/v1/uni-switch/quotas/codex-reset"},
		{"POST", "/v1/uni-switch/prove"},
	} {
		r := httptest.NewRequest(route.method, route.path, nil)
		r.RemoteAddr = "127.0.0.1:10000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, expected removed route", route.method, route.path, w.Code)
		}
	}
}

func TestRetiredRemoteCallerHeadersIgnored(t *testing.T) {
	fresh(t)
	r := httptest.NewRequest("POST", "/v1/responses", nil)
	r.RemoteAddr = "127.0.0.1:10000"
	r.Header.Set("User-Agent", "codex_cli_rs/0.150.0")
	wantAgent := agentOf(r)
	for _, header := range []string{"X-Uni-Switch-Agent", "X-Uni-Switch-Via", "X-Uni-Switch-Hops", "X-Uni-Switch-User-Agent", "X-Uni-Switch-Claude-Cache-TTL", "X-Uni-Switch-Searching", "X-Uni-Switch-Keep-Turn"} {
		r.Header.Set(header, "retired-forwarding-value")
	}
	withCaller(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := callerOf(r)
		if c.agent != wantAgent || c.ua != r.UserAgent() || c.claudeHere {
			t.Errorf("forwarding metadata changed the local client: %+v", c)
		}
		for name := range r.Header {
			if name != "User-Agent" {
				t.Errorf("forwarding header survived: %s", name)
			}
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
}
