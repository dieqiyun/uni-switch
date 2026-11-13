package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// Kai on Discord (0.1.1278): Claude Code asks claude/claude-opus-5-5 at
// high effort, both Claude accounts are out of their limit, and the Claude
// provider's own Fallback, a Codex subscription model, takes the request —
// streamed or not.
func TestClaudeAccountsSpentFallBackToCodex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	fresh(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	oauth := func(tok, plan string) map[string]any {
		return map[string]any{"claudeAiOauth": map[string]any{"accessToken": tok, "refreshToken": "r-" + tok, "expiresAt": far, "subscriptionType": plan}}
	}
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), mustJSON(oauth("tok-primary", "max")), 0o600)
	os.WriteFile(filepath.Join(home, ".claude.json"), mustJSON(map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "me@example.com", "accountUuid": "u-me"}}), 0o600)
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "auth.json"), mustJSON(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "me@example.com"}),
		"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "who": "acct-1"}),
		"refresh_token": "r-acct-1", "account_id": "acct-1"}}), 0o600)
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), mustJSON([]map[string]any{
		{"agent": "claude", "user": "me@example.com", "plan": "max", "on": true, "seen": time.Now(), "auth": oauth("tok-me", "max")},
		{"agent": "claude", "user": "pro@example.com", "plan": "pro", "on": true, "seen": time.Now(), "auth": oauth("tok-pro", "pro")},
	}), 0o600)
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	// every account is out of its limit: the CLI says so in a result
	script := `#!/bin/sh
if [ "$1" = auth ]; then echo '{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}'; exit 0; fi
TOK=; [ -n "$CLAUDE_CONFIG_DIR" ] && TOK=$(sed -n 's/.*"accessToken": *"\([^"]*\)".*/\1/p' "$CLAUDE_CONFIG_DIR/.credentials.json")
echo "${TOK:-own}" >> ` + log + `
while read -r line; do
  echo '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":1790700000}}'
  echo '{"type":"assistant","message":{"id":"x","model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"You'"'"'ve hit your limit · resets 5:58am"}]},"error":"rate_limit"}'
  echo '{"type":"result","subtype":"success","is_error":true,"result":"You'"'"'ve hit your limit · resets 5:58am"}'
done
`
	testenv.Program(t, filepath.Join(dir, "claude"), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var mu sync.Mutex
	var codex []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		codex = append(codex, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6.1-sol"}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[]}}`,
			`data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"pong"}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"pong"}]}}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	defer up.Close()
	old := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = old }()
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	if err := provider.Save(provider.Provider{ID: "claude", Fallback: []string{"codex/gpt-6.1-sol"}}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("claude")
	if err != nil || len(p.Fallback) != 1 {
		t.Fatalf("claude's fallback not kept: %+v %v", p.Fallback, err)
	}

	// Codex holds a ChatGPT plan's conversation to 272K; Opus holds 1M
	if err := provider.Save(provider.Provider{ID: "codex", Contexts: map[string]int{"gpt-6.1-sol": 272000}}); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("word ", 300000) // ~375K tokens by estimate
	for _, tc := range []struct {
		stream, text string
		again        bool
	}{
		{"true", "ping", false}, {"false", "ping", false}, {"true", "ping", true},
		// a conversation past Codex's window: the fallback is still asked
		// when every account with room is out of its limit, for the agent
		// to be told to compact rather than wait hours (Kai)
		{"true", long, false}, {"false", long, false},
	} {
		stream := tc.stream
		restingUntil.Lock()
		restingUntil.m = map[string]time.Time{}
		restingUntil.Unlock()
		os.Remove(log)
		mu.Lock()
		codex = nil
		mu.Unlock()
		s := New()
		if tc.again {
			for k := 0; k < 1; k++ {
				rq := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude/claude-opus-5-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"ping"}]}`))
				rq.Header.Set("User-Agent", "claude-cli/2.1.0 (external, cli)")
				s.Handler().ServeHTTP(httptest.NewRecorder(), rq)
			}
			mu.Lock()
			codex = nil
			mu.Unlock()
		}
		done := make(chan struct{})
		rec := httptest.NewRecorder()
		go func() {
			defer close(done)
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude/claude-opus-5-5","max_tokens":50,"stream":`+stream+`,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"messages":[{"role":"user","content":"`+tc.text+`"}]}`))
			req.Header.Set("User-Agent", "claude-cli/2.1.0 (external, cli)")
			s.Handler().ServeHTTP(rec, req)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatalf("stream=%s: no answer", stream)
		}
		b, _ := os.ReadFile(log)
		mu.Lock()
		got := len(codex)
		mu.Unlock()
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") || got == 0 {
			j, _ := json.Marshal(s.Trace(t.Context(), 0, 0))
			route := string(j)
			t.Errorf("stream=%s %d bytes again=%v: status %d: %.400s (claude runs %q, codex asked %d)\nroute %.3000s", stream, len(tc.text), tc.again, rec.Code, rec.Body.String(), b, got, route)
		}
	}
}
