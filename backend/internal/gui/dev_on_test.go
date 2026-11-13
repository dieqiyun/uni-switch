//go:build dev

package gui

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A backend restart cuts off a read in flight: the shell asks the backend
// that comes up again, and the page gets its answer, not a 502. A write
// still fails, since it may have landed.
func TestDevShellRetriesCutRead(t *testing.T) {
	var n atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 || r.Method == http.MethodPost {
			// the old backend going away mid-request
			c, _, _ := rw.(http.Hijacker).Hijack()
			c.Close()
			return
		}
		io.WriteString(rw, `{"ok":true}`)
	}))
	defer backend.Close()
	t.Setenv("UNI_SWITCH_DEV_ROLE", "shell")
	t.Setenv("UNI_SWITCH_DEV_BACKEND", strings.TrimPrefix(backend.URL, "http://"))
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Setenv("UNI_SWITCH_DEV_CONTROL", l.Addr().String())
	l.Close()
	shell := devShell(&host{})
	rec := httptest.NewRecorder()
	shell.ServeHTTP(rec, httptest.NewRequest("GET", "/api/usage", nil))
	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` || n.Load() != 2 {
		t.Fatalf("read: %d %q after %d tries", rec.Code, rec.Body, n.Load())
	}
	rec = httptest.NewRecorder()
	shell.ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings", strings.NewReader("{}")))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("write: %d", rec.Code)
	}
}
