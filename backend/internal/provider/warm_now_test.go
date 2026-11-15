package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Warm up now sends a ChatGPT account the very request the scheduled
// warm-up sends, to that account alone and with the warm-ups off, and
// says why it failed.
func TestWarmNowSendsTheScheduledWarmUp(t *testing.T) {
	signIn(t) // me@example.com, acct-1, lists gpt-5.5
	rememberLogins(true)
	var mu sync.Mutex
	var bodies []map[string]any
	var hdrs []http.Header
	refuse := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			// a week that hasn't started: nothing used, its reset a week off
			json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
				"primary_window":   map[string]any{"used_percent": 0, "limit_window_seconds": 18000, "reset_after_seconds": 18000},
				"secondary_window": map[string]any{"used_percent": 0, "limit_window_seconds": 604800, "reset_after_seconds": 604800}}})
		case "/backend-api/codex/responses":
			var m map[string]any
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &m)
			bodies, hdrs = append(bodies, m), append(hdrs, r.Header.Clone())
			if refuse {
				w.WriteHeader(http.StatusTooManyRequests)
				io.WriteString(w, `{"detail":"usage limit reached"}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	sent := func() int { mu.Lock(); defer mu.Unlock(); return len(bodies) }

	// the account as its name is typed, whatever its case
	if err := WarmNow(context.Background(), "codex", "ME@example.com"); err != nil {
		t.Fatalf("warm up now: %v", err)
	}
	if sent() != 1 {
		t.Fatalf("%d requests, want 1", sent())
	}
	// the scheduled warm-up's own request, for the same account
	w := codexWarmer{path: codexWarmPath(), now: time.Now, usage: codexWarmUsage, send: warmCodexLogin}
	if rs := w.warmNow(context.Background(), "week", nil); len(rs) != 1 || rs[0].Err != "" {
		t.Fatalf("scheduled: %+v", rs)
	}
	if sent() != 2 {
		t.Fatalf("%d requests, want 2", sent())
	}
	now, scheduled := bodies[0], bodies[1]
	if a, b := warmJSON(t, now), warmJSON(t, scheduled); a != b {
		t.Fatalf("Warm up now sent\n%s\nthe scheduled warm-up sends\n%s", a, b)
	}
	if now["model"] != "gpt-5.5" || now["store"] != false || now["instructions"] == nil {
		t.Fatalf("body: %v", now)
	}
	for _, h := range []string{"Authorization", "chatgpt-account-id", "originator", "User-Agent", "Content-Encoding"} {
		if hdrs[0].Get(h) != hdrs[1].Get(h) {
			t.Errorf("%s: %q, the scheduled warm-up sends %q", h, hdrs[0].Get(h), hdrs[1].Get(h))
		}
	}

	// refused: the vendor's reason comes back
	refuse = true
	err := WarmNow(context.Background(), "codex", "me@example.com")
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "usage limit reached") {
		t.Fatalf("refused: %v", err)
	}
	// no such account, or no warm-up for the agent: nothing sent
	n := sent()
	if err := WarmNow(context.Background(), "codex", "other@example.com"); err == nil {
		t.Fatal("warmed an account that isn't signed in")
	}
	if err := WarmNow(context.Background(), "copilot", "octocat"); err == nil {
		t.Fatal("warmed a Copilot account")
	}
	if sent() != n {
		t.Fatalf("%d requests for accounts that aren't there", sent()-n)
	}
}

// A Claude account's Warm up now is Claude Code's run the scheduled
// warm-up makes (WarmClaudeVia), in that account's own config directory.
func TestWarmNowClaudeRunsClaudeCode(t *testing.T) {
	_, side := claudeAuthFixture(t, "")
	rememberLogins(true) // the one Claude Code is signed in to, listed beside it
	old := claudeWarmSend
	t.Cleanup(func() { claudeWarmSend = old })
	claudeWarmSend = nil
	if err := WarmNow(context.Background(), "claude", side.User); err == nil {
		t.Fatal("warmed with no Claude Code to send it")
	}
	var mu sync.Mutex
	var dirs []string
	fail := error(nil)
	WarmClaudeVia(func(_ context.Context, dir string) error {
		mu.Lock()
		defer mu.Unlock()
		dirs = append(dirs, dir)
		return fail
	})
	if err := WarmNow(context.Background(), "claude", side.User); err != nil {
		t.Fatalf("saved account: %v", err)
	}
	if err := WarmNow(context.Background(), "claude", "own@example.com"); err != nil {
		t.Fatalf("own account: %v", err)
	}
	if len(dirs) != 2 || dirs[0] == "" || dirs[1] != "" {
		t.Fatalf("Claude Code run in %q, want the saved account's directory, then its own", dirs)
	}
	fail = errors.New("Claude Code: You've hit your session limit")
	if err := WarmNow(context.Background(), "claude", "own@example.com"); err == nil || !strings.Contains(err.Error(), "session limit") {
		t.Fatalf("failed run: %v", err)
	}
}

// An account whose sign-in lapsed is sent nothing, and says so.
func TestWarmNowLapsedAccount(t *testing.T) {
	_, side := claudeAuthFixture(t, "Failed to authenticate: OAuth token revoked")
	old := claudeWarmSend
	t.Cleanup(func() { claudeWarmSend = old })
	ran := 0
	WarmClaudeVia(func(context.Context, string) error { ran++; return nil })
	err := WarmNow(context.Background(), "claude", side.User)
	if err == nil || !strings.Contains(err.Error(), "signing in again") || ran != 0 {
		t.Fatalf("lapsed: %v, %d runs", err, ran)
	}
}

func warmJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
