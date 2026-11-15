package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Codex's rule for a refresh refused for good, as codex-rs
// login/src/auth/manager.rs has it, and the code read where Codex reads
// it. A 401 with no code (a proxy's page), a 403, a 5xx, a rate limit and
// any other 400 say nothing of the sign-in.
func TestCodexRefreshGoneRule(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   bool
	}{
		{401, `{"error":{"message":"Your refresh token has been invalidated.","code":"refresh_token_invalidated"}}`, true},
		{400, `{"error":{"code":"refresh_token_expired"}}`, true},
		{400, `{"error":"refresh_token_reused"}`, true},
		{403, `{"code":"REFRESH_TOKEN_INVALIDATED"}`, true},
		{400, `{"error":"invalid_grant","error_description":"Refresh token has been revoked"}`, true},
		{401, `{"error":"unauthorized_client"}`, true},
		{401, `<html>401 Authorization Required</html>`, false},
		{401, ``, false},
		{400, `{"error":"invalid_request"}`, false},
		{403, `{"error":"invalid_grant"}`, false},
		{500, `{"error":"server_error"}`, false},
		{502, `upstream connect error`, false},
		{429, `{"error":{"code":"rate_limit_exceeded"}}`, false},
	} {
		if got := codexRefreshGone(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%d %s: %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

// Codex's own sign-in, revoked (jjt on Discord, #4308): uni-switch's usage read
// is turned away 401 token_revoked, its refresh is refused for good, and
// the sign-in is gone to uni-switch (CodexSignInGone), kept across a restart.
// A refresh that fails otherwise marks nothing, nor does a refusal of a
// token Codex has replaced meanwhile. A sign-in made again is not gone,
// with no step of the user's. A record that can't be read says nothing.
func TestCodexSignInGoneWhenRefusedForGood(t *testing.T) {
	home := signIn(t)
	resetRevoked() // marks are read from this HOME's logins.json
	t.Cleanup(resetRevoked)
	rememberLogins(true) // this HOME's sign-ins, not an earlier test's
	loginUsageCache.Lock()
	loginUsageCache.m = nil
	loginUsageCache.Unlock()
	t.Cleanup(func() { codexWasGone.Store(false) })
	dir := filepath.Join(home, ".codex")
	auth := func() []byte {
		b, err := os.ReadFile(filepath.Join(dir, "auth.json"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	gone := func() (bool, bool) { return CodexSignInGone(dir, auth()) }

	var answer atomic.Value // the token endpoint's: "gone", "down", "rotated" or "ok"
	answer.Store("down")
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"Encountered invalidated oauth token for user, failing request","type":null,"code":"token_revoked","param":null},"status":401}`))
	}))
	defer usage.Close()
	refresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch answer.Load() {
		case "down":
			w.WriteHeader(502)
			w.Write([]byte(`upstream connect error`))
		case "rotated":
			// Codex refreshed first: its new tokens are in auth.json by
			// the time uni-switch's spent one is refused
			writeFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
				"auth_mode": "chatgpt",
				"tokens": map[string]any{
					"id_token":      fakeJWT(map[string]any{"email": "me@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-1"}}),
					"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "n": "codex"}),
					"refresh_token": "r-codex", "account_id": "acct-1",
				},
			})
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"code":"refresh_token_reused"}}`))
		default:
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"Your refresh token has been invalidated.","code":"refresh_token_invalidated"}}`))
		}
	}))
	defer refresh.Close()
	oldBase, oldURL := CodexBase, codexTokenURL
	CodexBase, codexTokenURL = usage.URL+"/backend-api/codex", refresh.URL
	t.Cleanup(func() { CodexBase, codexTokenURL = oldBase, oldURL })
	read := func() {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		LoginUsage(context.Background(), "codex")
	}

	if g, known := gone(); g || !known {
		t.Fatalf("before anything was refused: gone %v known %v", g, known)
	}
	read() // the token endpoint down: unknown
	if g, _ := gone(); g {
		t.Fatal("a 502 from the token endpoint taken as the sign-in gone")
	}

	answer.Store("rotated")
	read()
	if g, _ := gone(); g {
		t.Fatal("a token Codex replaced meanwhile marked gone")
	}

	answer.Store("gone")
	read()
	if g, known := gone(); !g || !known {
		t.Fatalf("refused for good: gone %v known %v", g, known)
	}
	noteCodexGoneNow()
	if !codexWasGone.Load() {
		t.Fatal("the loop's look doesn't see it gone")
	}
	// the record names no token
	if b, _ := os.ReadFile(codexGonePath()); json.Valid(b) && len(b) > 0 {
		var m map[string]string
		json.Unmarshal(b, &m)
		if m[filepath.Clean(dir)] != codexGoneHash("r-codex") {
			t.Fatalf("record %s", b)
		}
	}

	// unreadable: unknown, never gone
	p := codexGonePath()
	saved, _ := os.ReadFile(p)
	os.WriteFile(p, []byte("{cut off"), 0o600)
	if g, known := gone(); g || known {
		t.Fatalf("unreadable record: gone %v known %v, want unknown", g, known)
	}
	os.WriteFile(p, saved, 0o600)

	// signed in again: another refresh token, not gone, and the loop says so
	codexSignIn(t, home, "me@example.com", "r-new")
	if g, known := gone(); g || !known {
		t.Fatalf("signed in again: gone %v known %v", g, known)
	}
	noteCodexGoneNow()
	if codexWasGone.Load() {
		t.Fatal("the loop's look still sees it gone after a sign-in")
	}
}
