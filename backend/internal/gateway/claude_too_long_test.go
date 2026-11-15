package gateway

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// Claude Code turning a conversation away itself, "Prompt is too long"
// with no status from Anthropic, is a 400 that says so, not a 502 that
// reads as an upstream fault: pi on claude/claude-sonnet-5-5 was answered
// 502 at about 180K tokens and neither compacted nor showed why (#2162).
func TestClaudeCodesOwnTooLongIsA400(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	dir := t.TempDir()
	testenv.Program(t, filepath.Join(dir, "claude"), `#!/bin/sh
read -r line
echo '{"type":"system","subtype":"init","session_id":"s"}'
echo '{"type":"result","subtype":"success","is_error":true,"result":"Prompt is too long","session_id":"s"}'
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, c := range []struct {
		from provider.Protocol
		path string
		body string
	}{
		{provider.Anthropic, "/v1/messages", `{"model":"claude-sonnet-5-5","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`},
		{provider.Anthropic, "/v1/messages", `{"model":"claude-sonnet-5-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`},
		{provider.Chat, "/v1/chat/completions", `{"model":"claude-sonnet-5-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`},
		{provider.Chat, "/v1/chat/completions", `{"model":"claude-sonnet-5-5","messages":[{"role":"user","content":"hi"}]}`},
	} {
		s := New()
		p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
		rec := httptest.NewRecorder()
		var u Usage
		s.serveClaudeSubscription(rec, httptest.NewRequest("POST", c.path, strings.NewReader(c.body)), c.from, p, "claude-sonnet-5-5", []byte(c.body), &u)
		s.subscription.abortAll()
		out := rec.Body.String()
		if rec.Code != 400 {
			t.Errorf("%s %s: status %d, want 400: %s", c.from, c.body, rec.Code, out)
		}
		// the words a client knows an overflow by stay first, and what to do follows
		if !strings.Contains(out, "Claude Code: Prompt is too long") || !strings.Contains(out, "extra usage") {
			t.Errorf("%s: %s", c.from, out)
		}
	}
}

// A run's error with a status, or one about quota, keeps its own: only
// Claude Code's own too-long check is made a 400.
func TestBridgeFailedKeepsOtherStatuses(t *testing.T) {
	for _, c := range []struct {
		name, msg string
		status    int
		want      int
	}{
		{"Claude Code", "prompt is too long: 213462 tokens > 200000 maximum", 400, 400},
		{"Claude Code", "Extra usage is required for long context", 429, 429},
		{"Claude Code", "You've hit your limit", 0, 429},
		{"Claude Code", "Internal server error", 0, 502},
		{"Claude Code", "Prompt is too long", 0, 400},
		{"Codex", "Prompt is too long", 0, 502},
	} {
		if got, _ := bridgeFailed(c.name, c.msg, c.status); got != c.want {
			t.Errorf("%s %q %d: %d, want %d", c.name, c.msg, c.status, got, c.want)
		}
	}
}
