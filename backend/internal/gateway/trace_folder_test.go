package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestShortFolder(t *testing.T) {
	for dir, want := range map[string]string{
		"/Users/someone/secret/workspace/uni-switch":  "workspace/uni-switch",
		"/Users/someone/secret/workspace/uni-switch/": "workspace/uni-switch",
		`C:\Users\someone\src\uni-switch`:             "src/uni-switch",
		`D:\uni-switch`:                               "uni-switch",
		`C:\`:                                         "",
		`\\server\share\team\uni-switch`:              "team/uni-switch",
		"/uni-switch":                                 "uni-switch",
		"/":                                           "",
		"":                                            "",
		"/home/u/my project/中文 目录":                    "my project/中文 目录",
	} {
		if got := shortFolder(dir); got != want {
			t.Errorf("shortFolder(%q) = %q, want %q", dir, got, want)
		}
	}
}

// A route keeps the last two levels of the folder its agent says it works
// in, and never the rest of the path (#1068): in the trace the page and
// the agents' callers read, and in the history on disk. A request of the
// same session that says none, as Claude Code's title request doesn't,
// takes the session's; one with no session id has none.
func TestRouteKeepsItsFolderShort(t *testing.T) {
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
	send := func(body, session string) Route {
		t.Helper()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("User-Agent", "claude-cli/2.1.296 (external, cli)")
		if session != "" {
			req.Header.Set("x-claude-code-session-id", session)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		rs := s.Trace(context.Background(), 0, 0).Routes
		return rs[len(rs)-1]
	}
	// Claude Code's main request: its environment in a system block
	turn := `{"model":"plan/m1","stream":true,"max_tokens":100,"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."},{"type":"text","text":"# Environment\nYou have been invoked in the following environment: \n - Primary working directory: /Users/someone/secret/workspace/uni-switch\n - Is a git repository: true\n - Platform: darwin\n"}],"messages":[{"role":"user","content":"hi"}]}`
	// its title request: no environment
	title := `{"model":"plan/m1","stream":true,"max_tokens":100,"system":[{"type":"text","text":"Generate a concise, sentence-case title (3-7 words) that captures the main topic or goal of this coding session."}],"messages":[{"role":"user","content":"hi"}]}`

	if r := send(turn, "s1"); r.Folder != "workspace/uni-switch" {
		t.Fatalf("the turn's folder: %q", r.Folder)
	}
	if r := send(title, "s1"); r.Folder != "workspace/uni-switch" {
		t.Fatalf("a request of the session that says no folder: %q", r.Folder)
	}
	if r := send(title, "s2"); r.Folder != "" {
		t.Fatalf("another session's: %q", r.Folder)
	}
	if r := send(title, ""); r.Folder != "" {
		t.Fatalf("no session id: %q", r.Folder)
	}
	// the folder said last goes: the agent moved
	moved := strings.Replace(turn, "/Users/someone/secret/workspace/uni-switch", `C:\\Users\\someone\\src\\dial`, 1)
	if r := send(moved, "s1"); r.Folder != "src/dial" {
		t.Fatalf("moved: %q", r.Folder)
	}
	if r := send(title, "s1"); r.Folder != "src/dial" {
		t.Fatalf("after the move: %q", r.Folder)
	}

	// what the page gets, and what the history keeps on disk
	now := historyNoon(t)
	for _, r := range s.Trace(context.Background(), 0, 0).Routes {
		b, _ := json.Marshal(r)
		for _, leak := range []string{"someone", "secret", "Users"} {
			if strings.Contains(string(b), leak) {
				t.Fatalf("the route carries the folder's path (%s): %s", leak, b)
			}
		}
		r.Time = now
		saveRoute(r)
	}
	_, kept, _ := History(now.Format(dayForm))
	var folders []string
	for _, r := range kept {
		folders = append(folders, r.Folder)
	}
	if got := strings.Join(folders, ","); !strings.Contains(got, "workspace/uni-switch") || !strings.Contains(got, "src/dial") {
		t.Fatalf("the history's folders: %q", got)
	}
}
