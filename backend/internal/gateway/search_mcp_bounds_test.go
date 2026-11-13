package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth/mcpauthtest"
)

// A body past an MCP route's limit is refused whole (#1766, ggbdpq): a
// valid JSON-RPC message padded with whitespace past the limit, or followed
// by a second message there, was read up to the limit and run. The length
// is told from the bytes read, not Content-Length, which a chunked body
// doesn't have. A body exactly at the limit still works.
func TestOwnMCPRejectsOversizedBody(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		limit int
	}{
		{"vision", VisionMCPPath, visionMCPBody},
		{"search", SearchMCPPath, 1 << 20},
		{"tools", ToolsMCPPath, toolsMCPBody},
		{"hub", HubMCPPath + "claude", hubBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, upstream := eyed(t)
			h := localGuard(s.Handler())
			call := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
			if tc.path == VisionMCPPath {
				call = analyzeCall("https://image.invalid/screenshot.png", "Read the screenshot")
			}
			bounded := call + strings.Repeat(" ", tc.limit-len(call))
			post := func(body string, length int64) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
				r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:3425"
				r.ContentLength = length
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			if w := post(bounded, -1); w.Code != http.StatusOK {
				t.Fatalf("a body exactly at the limit must work: %d %s", w.Code, w.Body)
			}
			before := len(upstream.sent("eye"))
			for _, suffix := range []string{" ", `{"jsonrpc":"2.0","id":2,"method":"ping"}`} {
				body := bounded + suffix
				for _, length := range []int64{-1, int64(len(body))} {
					if w := post(body, length); w.Code != http.StatusRequestEntityTooLarge {
						t.Errorf("a %d-byte body (Content-Length %d) past the %d-byte limit: got HTTP %d, want 413; reply %.200s", len(body), length, tc.limit, w.Code, w.Body)
					}
				}
			}
			if after := len(upstream.sent("eye")); after != before {
				t.Errorf("oversized bodies must not run an image call: before %d, after %d", before, after)
			}
		})
	}
}

// The same holds for a signed-in MCP server uni-switch proxies: a body cut at
// 32 MiB went to the server, with the user's sign-in, as a shorter message.
func TestMCPProxyRejectsOversizedBody(t *testing.T) {
	fresh(t)
	f := mcpauthtest.New(t)
	f.SignIn(t, "neon")
	gw := httptest.NewServer(New().Handler())
	defer gw.Close()
	const limit = 32 << 20
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`
	bounded := call + strings.Repeat(" ", limit-len(call))
	post := func(body string) int {
		t.Helper()
		// no Content-Length: the body goes chunked
		req, _ := http.NewRequest("POST", gw.URL+"/mcp/neon", struct{ *strings.Reader }{strings.NewReader(body)})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(bounded); code != http.StatusOK {
		t.Fatalf("a body exactly at the limit must go through: %d", code)
	}
	n := len(f.Calls)
	if code := post(bounded + " "); code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body one byte past the limit: got HTTP %d, want 413", code)
	}
	if len(f.Calls) != n {
		t.Errorf("an oversized body reached the server: %q", f.Calls[n:])
	}
}
