package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// revokedUpstream stands in for the ChatGPT backend with the sign-in of
// the access token revoked revoked, as ChatGPT answers one (#1793); tried
// is the accounts asked a turn, and with reset starts the list again.
func revokedUpstream(t *testing.T, revoked string) (tried func(reset bool) []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Encoding") == "zstd" {
			d, _ := zstd.NewReader(nil)
			b, _ = d.DecodeAll(b, nil)
			d.Close()
		}
		var q struct {
			Input any `json:"input"`
		}
		json.Unmarshal(b, &q)
		gone := r.Header.Get("Authorization") == "Bearer "+revoked
		// a sign-in's warm-up ("hi", codex_warmup.go) isn't a turn
		if q.Input != "hi" {
			mu.Lock()
			asked = append(asked, r.Header.Get("chatgpt-account-id"))
			mu.Unlock()
		}
		if gone {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"Encountered invalidated oauth token for user, failing request","code":"token_revoked"},"status":401}`)
			return
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.5"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return func(reset bool) []string {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(asked)
		if reset {
			asked = nil
		}
		return out
	}
}

// codexAccessToken is the access token in the test's Codex sign-in.
func codexAccessToken(t *testing.T) string {
	t.Helper()
	var a struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".codex", "auth.json"))
	if err != nil || json.Unmarshal(b, &a) != nil || a.Tokens.AccessToken == "" {
		t.Fatalf("no Codex sign-in: %v %s", err, b)
	}
	return a.Tokens.AccessToken
}

func codexLapsed(t *testing.T, user string) string {
	t.Helper()
	for _, l := range provider.Logins("codex") {
		if l.User == user {
			return l.Lapsed
		}
	}
	t.Fatalf("no codex account %s in %+v", user, provider.Logins("codex"))
	return ""
}

// A ChatGPT account whose sign-in OpenAI revoked (#1793, ysicing: 401
// token_revoked, asked again on every request) is passed over for the next
// account at once, without a rest, and isn't asked again while it has that
// sign-in: its row says to sign in again. Signed in anew, it is asked, and
// served, the mark is gone.
func TestRevokedSignInPassedOverTillSignedInAgain(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	tried := revokedUpstream(t, codexAccessToken(t))
	turn := func(name, input string, want string) {
		t.Helper()
		tried(true)
		s := New()
		rec := codexPostTo(s, `{"model":"gpt-5.5","stream":true,"input":"`+input+`"}`)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") {
			t.Fatalf("%s: status %d: %s", name, rec.Code, rec.Body.String())
		}
		if got := strings.Join(tried(false), ","); got != want {
			t.Fatalf("%s: asked %s, not %s: %+v", name, got, want, lastRoute(s).Tries)
		}
		// passed over for its sign-in, not resting: a rest would end, and
		// the account be asked again (the screenshot's rows of 401s)
		restingUntil.Lock()
		rests := len(restingUntil.m)
		restingUntil.Unlock()
		if rests != 0 {
			t.Fatalf("%s: the revoked account rests, to be asked again after", name)
		}
	}

	turn("first turn", "ping", "acct-1,acct-2")
	if got := codexLapsed(t, "me@example.com"); !strings.Contains(got, "sign in again") {
		t.Fatalf("the revoked account isn't said signed out: %q", got)
	}
	for _, in := range []string{"ping", "another", "a third"} {
		turn("turn "+in, in, "acct-2")
	}
	// kept across a restart: read from logins.json again
	provider.ForgetAccounts()
	turn("after a restart", "ping", "acct-2")
	if got := codexLapsed(t, "me@example.com"); got == "" {
		t.Fatal("the mark is gone after a restart")
	}

	// Codex signed in again: a new token, asked and served
	claims, _ := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "who": "acct-1", "n": 2})
	auth := map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      "h." + base64.RawURLEncoding.EncodeToString(mustJSON(map[string]any{"email": "me@example.com"})) + ".s",
		"access_token":  "h." + base64.RawURLEncoding.EncodeToString(claims) + ".s",
		"refresh_token": "r-acct-1b", "account_id": "acct-1"}}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".codex", "auth.json"), mustJSON(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	turn("signed in again", "a new conversation", "acct-1")
	if got := codexLapsed(t, "me@example.com"); got != "" {
		t.Fatalf("still said signed out after serving: %q", got)
	}
}

// Compaction goes to an account of uni-switch's alone, with the one Codex is
// signed in to paused (#263): one whose sign-in OpenAI revoked (#1793) is
// marked by its 401 there too, and the next compaction goes to another,
// not to it again.
func TestRevokedSignInNotCompactedOnAgain(t *testing.T) {
	codexSignedIn(t, "spare@example.com", "third@example.com")
	var mu sync.Mutex
	var tried []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tried = append(tried, r.Header.Get("chatgpt-account-id"))
		mu.Unlock()
		if r.Header.Get("chatgpt-account-id") == "acct-2" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"Encountered invalidated oauth token for user, failing request","code":"token_revoked"},"status":401}`)
			return
		}
		io.WriteString(w, `{"output":[{"type":"compaction","encrypted_content":"gAAAAAx"}]}`)
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.SetLoginOn("codex", "me@example.com", false); err != nil {
		t.Fatal(err)
	}
	compact := func() (int, string) {
		t.Helper()
		mu.Lock()
		tried = nil
		mu.Unlock()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", CodexPath+"/responses/compact", strings.NewReader(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"ping"}]}`))
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		req.Header.Set("chatgpt-account-id", "acct-1")
		New().Handler().ServeHTTP(rec, req)
		mu.Lock()
		defer mu.Unlock()
		return rec.Code, strings.Join(tried, ",")
	}
	if code, asked := compact(); code != 401 || asked != "acct-2" {
		t.Fatalf("first: %d, asked %s", code, asked)
	}
	if got := codexLapsed(t, "spare@example.com"); !strings.Contains(got, "sign in again") {
		t.Fatalf("not said signed out: %q", got)
	}
	for range 2 {
		if code, asked := compact(); code != 200 || asked != "acct-3" {
			t.Fatalf("after: %d, asked %s", code, asked)
		}
	}
}
