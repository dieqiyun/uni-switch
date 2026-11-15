package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Claude Code subagent's request goes under its conversation's session
// id; its route says whose it is (x-claude-code-agent-id), so the Context
// page doesn't take its smaller prompt for the conversation compacting.
// The conversation's own turns name none.
func TestRouteSaysItsSubagent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
		io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Chat: srv.URL + "/v1", Models: []string{"m1"}, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	body := `{"model":"plan/m1","stream":true,"max_tokens":100,"system":"You are Claude Code.","tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`
	for _, sub := range []string{"", "agent-a1b2c3"} {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("User-Agent", "claude-cli/2.1.295 (external, cli)")
		req.Header.Set("x-claude-code-session-id", "cc-1")
		if sub != "" {
			req.Header.Set("x-claude-code-agent-id", sub)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	got := map[string]int{}
	for _, r := range s.Trace(t.Context(), 0, 0).Routes {
		if r.Session != "cc-1" {
			t.Fatalf("route %d session %q", r.ID, r.Session)
		}
		got[r.Sub]++
	}
	if got[""] != 1 || got["agent-a1b2c3"] != 1 {
		t.Fatalf("routes by subagent %v, want one of the conversation's and one of agent-a1b2c3's", got)
	}
}
