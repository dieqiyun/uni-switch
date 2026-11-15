package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /mcp/run/<name> starts a command the user gave their agents: only an
// agent on this computer has it run, never a web page (an Origin, a name
// of the page's own pointed at 127.0.0.1), a request a proxy or tunnel
// here passed on, or another machine.
func TestRunMCPThisComputerOnly(t *testing.T) {
	fresh(t)
	var ran []string
	was := RunMCP
	RunMCP = func(w http.ResponseWriter, r *http.Request, name string) {
		ran = append(ran, name)
		w.WriteHeader(http.StatusAccepted)
	}
	t.Cleanup(func() { RunMCP = was })
	h := New().Handler()
	for _, c := range []struct {
		remote, host, origin, fwd string
	}{
		{"127.0.0.1:5000", "127.0.0.1:3425", "https://evil.example", ""},
		{"127.0.0.1:5000", "127.0.0.1:3425", "http://localhost:3425", ""},
		{"127.0.0.1:5000", "evil.example:3425", "", ""},
		{"127.0.0.1:5000", "127.0.0.1:3425", "", "203.0.113.9"},
		{"192.168.1.20:5000", "192.168.1.10:3425", "", ""},
	} {
		r := httptest.NewRequest("POST", "/mcp/run/fs", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))
		r.RemoteAddr, r.Host = c.remote, c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.fwd != "" {
			r.Header.Set("X-Forwarded-For", c.fwd)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || len(ran) != 0 {
			t.Fatalf("%+v: got %d, ran %v", c, w.Code, ran)
		}
	}
	for _, host := range []string{"127.0.0.1:3425", "localhost:3425", "[::1]:3425"} {
		r := httptest.NewRequest("POST", "/mcp/run/fs", strings.NewReader(`{}`))
		r.RemoteAddr, r.Host = "127.0.0.1:5000", host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusAccepted {
			t.Fatalf("%s: got %d %s", host, w.Code, w.Body)
		}
	}
	if strings.Join(ran, " ") != "fs fs fs" {
		t.Fatalf("ran %v", ran)
	}
}
