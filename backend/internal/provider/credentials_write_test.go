package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/steady"
)

// fakeCodexRefresh answers a refresh with a fresh access token and the
// refresh token rotated to "<old>-2", as OpenAI does.
func fakeCodexRefresh(t *testing.T) {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": body["refresh_token"] + "-2"})
	}))
	t.Cleanup(fake.Close)
	old := codexTokenURL
	codexTokenURL = fake.URL
	t.Cleanup(func() { codexTokenURL = old })
}

// dueCodexAuth writes an auth.json, as Codex CLI does, whose access token
// is about to expire, so the next codexToken refreshes it.
func dueCodexAuth(t *testing.T) (path string, was []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), ".codex", "auth.json")
	writeFile(t, path, map[string]any{
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Minute).Unix())}),
			"refresh_token": "r-work",
			"account_id":    "acct-1",
		},
		"last_refresh": "2026-10-14T00:00:00Z",
	})
	was, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, was
}

// #2239: the rotated tokens a Codex refresh writes into the user's own
// auth.json go into a file beside it, flushed to the disk, before it is
// renamed over the old one. The old file is never truncated in place: a
// machine going down mid-write, or a flush that fails, leaves the sign-in
// as it was rather than an empty or zeroed file (#1505).
func TestCodexRefreshFlushesAuthJSONBeforeReplacingIt(t *testing.T) {
	isolate(t)
	fakeCodexRefresh(t)
	defer func() { steady.Sync = (*os.File).Sync }()

	// a flush that fails leaves auth.json whole, and the request served
	path, was := dueCodexAuth(t)
	steady.Sync = func(*os.File) error { return errors.New("the disk went away") }
	if tok, _, err := codexToken(context.Background(), filepath.Dir(path)); err != nil || tok == "" {
		t.Fatalf("codexToken: %q, %v", tok, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != string(was) {
		t.Fatalf("a flush that failed left auth.json %q (%v), want the sign-in as it was", b, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".auth.json.*")); len(left) != 0 {
		t.Fatalf("temp files left beside auth.json: %v", left)
	}

	// a flush that works: flushed while auth.json still holds the old
	// sign-in whole, then renamed in, every other field kept
	path, was = dueCodexAuth(t)
	flushed := false
	dir, _ := filepath.EvalSymlinks(filepath.Dir(path)) // /var is /private/var on macOS
	steady.Sync = func(f *os.File) error {
		if filepath.Dir(f.Name()) != dir || filepath.Base(f.Name()) == "auth.json" {
			t.Errorf("flushed %s, not a file beside %s", f.Name(), path)
		}
		if b, _ := os.ReadFile(path); string(b) != string(was) {
			t.Errorf("auth.json was changed before the new sign-in was flushed: %q", b)
		}
		flushed = true
		return f.Sync()
	}
	if _, _, err := codexToken(context.Background(), filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if !flushed {
		t.Fatal("auth.json was written without the new sign-in being flushed to the disk first")
	}
	var got map[string]any
	b, _ := os.ReadFile(path)
	if json.Unmarshal(b, &got) != nil {
		t.Fatalf("auth.json after the refresh: %q", b)
	}
	toks, _ := got["tokens"].(map[string]any)
	if toks["refresh_token"] != "r-work-2" || toks["account_id"] != "acct-1" || got["last_refresh"] == nil {
		t.Fatalf("auth.json after the refresh: %s", b)
	}
	if fi, err := os.Stat(path); err != nil || (os.PathSeparator == '/' && fi.Mode().Perm() != 0o600) {
		t.Fatalf("auth.json's mode: %v, %v", fi, err)
	}
}

// The same for Claude Code's own .credentials.json, written when uni-switch
// switches the sign-in Claude Code is on: a write whose flush failed is
// an error and leaves the file as it was.
func TestClaudeCredentialFlushesBeforeReplacing(t *testing.T) {
	isolate(t)
	defer func() { steady.Sync = (*os.File).Sync }()
	path := filepath.Join(t.TempDir(), ".claude", ".credentials.json")
	writeFile(t, path, map[string]any{"claudeAiOauth": map[string]any{"accessToken": "sk-ant-oat01-old", "refreshToken": "sk-ant-ort01-old"}})
	was, _ := os.ReadFile(path)
	c, ok := parseClaudeCredentials([]byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-new","refreshToken":"sk-ant-ort01-new","expiresAt":1999999999000}}`))
	if !ok {
		t.Fatal("credentials don't parse")
	}
	loc := claudeCredentialLocation{path: path}

	steady.Sync = func(*os.File) error { return errors.New("the disk went away") }
	if err := writeClaudeCredential(loc, c); err == nil {
		t.Error("a write whose flush failed reported as saved")
	}
	if b, _ := os.ReadFile(path); string(b) != string(was) {
		t.Fatalf("a flush that failed left the credentials %q, want them as they were", b)
	}

	flushed := false
	steady.Sync = func(f *os.File) error {
		if b, _ := os.ReadFile(path); string(b) != string(was) {
			t.Errorf("the credentials were changed before the new ones were flushed: %q", b)
		}
		flushed = true
		return f.Sync()
	}
	if err := writeClaudeCredential(loc, c); err != nil {
		t.Fatal(err)
	}
	if !flushed {
		t.Fatal("the credentials file was written without the new sign-in being flushed to the disk first")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "sk-ant-ort01-new") {
		t.Fatalf("written: %q", b)
	}
}
