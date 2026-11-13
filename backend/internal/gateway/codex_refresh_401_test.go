package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A 401 that says the token has expired and asks for a refresh (#2973) is
// one a refresh mends, though the token's JWT exp is still ahead: the
// sign-in is renewed where it is kept, and the next request goes out with
// the fresh token — not recorded as a failure of other kinds and rested
// while the account still has its allowance.
func TestCodexExpired401RefreshesLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	t.Cleanup(func() {
		restingUntil.Lock()
		restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
		restingUntil.Unlock()
	})

	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	// the token on disk expires an hour from now: good here, retired at
	// the vendor, whose answer asks for a refresh (#2973)
	stale := claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "v": "stale"})
	auth, err := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "c@example.com"}),
		"access_token":  stale,
		"refresh_token": "r-own",
		"account_id":    "acct-c"}})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	refreshed, served := 0, []string{}
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		refreshed++
		mu.Unlock()
		// the rotation OpenAI's answer is: a fresh access token, and
		// another refresh token for the spent one
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "v": "fresh"}),
			"refresh_token": body["refresh_token"] + "-2"})
	}))
	defer tokens.Close()
	defer provider.CodexTokenURLForTest(tokens.URL)()

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		served = append(served, tok)
		mu.Unlock()
		if tok == stale {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"Your authentication token has expired. Please try refreshing it."}}`)
			return
		}
		w.Header()["Content-Type"] = nil // as the ChatGPT backend sends it
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1"}}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}`))
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	if code, _ := post(t, "/v1/responses", `{"model":"codex/gpt-5.5","input":"ping"}`); code != 401 {
		t.Fatalf("first request: %d, want the vendor's 401 told", code)
	}
	mu.Lock()
	got, hits := append([]string(nil), served...), refreshed
	mu.Unlock()
	if hits != 1 {
		t.Fatalf("the vendor said the token expired and asked for a refresh: the sign-in was renewed %d times, want once", hits)
	}
	if len(got) != 1 || got[0] != stale {
		t.Fatalf("the backend was sent %q, want the stale token once", got)
	}

	// the backoff a refusal of other kinds rests the account by is over
	noRests()
	if code, body := post(t, "/v1/responses", `{"model":"codex/gpt-5.5","input":"ping"}`); code != 200 || !strings.Contains(body, `"status":"completed"`) {
		t.Fatalf("after the refresh: %d %s", code, body)
	}
	mu.Lock()
	got, hits = append([]string(nil), served...), refreshed
	mu.Unlock()
	if hits != 1 {
		t.Fatalf("the sign-in was renewed %d times after a request was served, want the one", hits)
	}
	if len(got) != 2 || got[1] == stale {
		t.Fatalf("the next request went out with %q, want the fresh token", got)
	}
	// the rotation is back where Codex keeps its sign-in, for Codex to find
	b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil || !strings.Contains(string(b), "r-own-2") {
		t.Fatalf("the rotated refresh token didn't reach auth.json: %q %v", b, err)
	}
}

// A 401 that says nothing of an expired token leaves the sign-in as it is:
// a revoked one is #1793's to mark, any other is no refresh's to mend.
func TestCodexOther401DoesntRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	t.Cleanup(func() {
		restingUntil.Lock()
		restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
		restingUntil.Unlock()
	})

	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth, err := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "c@example.com"}),
		"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
		"refresh_token": "r-own",
		"account_id":    "acct-c"}})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	refreshed := 0
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		refreshed++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"access_token": claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()})})
	}))
	defer tokens.Close()
	defer provider.CodexTokenURLForTest(tokens.URL)()

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Incorrect API key provided"}}`)
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	post(t, "/v1/responses", `{"model":"codex/gpt-5.5","input":"ping"}`)
	mu.Lock()
	hits := refreshed
	mu.Unlock()
	if hits != 0 {
		t.Fatalf("a 401 that says nothing of an expired token renewed the sign-in %d times", hits)
	}
}
