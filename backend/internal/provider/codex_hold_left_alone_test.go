package provider

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// sherlock_23333 on X: the ChatGPT account Codex is signed in to ran out,
// and the Codex app greyed Send for uni-switch's models too, through restarts.
// With Codex switched off or removed in uni-switch nothing read the account,
// so uni-switch never became Codex's provider while the app held it. Its hold
// is read still, while Codex takes uni-switch's models, and only then: the
// account Codex is on alone, its sign-in never renewed by uni-switch.
func TestCodexHoldReadLeftAlone(t *testing.T) {
	home := signIn(t)
	rememberLogins(true)
	body := heldUsage(t)
	var usage, refresh atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			usage.Add(1)
			json.NewEncoder(w).Encode(body)
		case "/oauth/token":
			refresh.Add(1)
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(fake.Close)
	oldBase, oldTok := CodexBase, codexTokenURL
	CodexBase, codexTokenURL = fake.URL+"/backend-api/codex", fake.URL+"/oauth/token"
	matters := false
	oldHook := CodexHoldMatters
	CodexHoldMatters = func() bool { return matters }
	t.Cleanup(func() { CodexBase, codexTokenURL, CodexHoldMatters = oldBase, oldTok, oldHook })
	fresh := func() {
		loginUsageCache.Lock()
		loginUsageCache.m, loginUsageCache.pending = nil, nil
		loginUsageCache.Unlock()
	}

	for _, c := range []struct {
		name string
		p    map[string]any
	}{
		{"switched off", map[string]any{"id": "codex", "off": true}},
		{"removed from uni-switch", map[string]any{"id": "codex", "hidden": true}},
	} {
		writeFile(t, Path(), map[string]any{"providers": []map[string]any{c.p}})
		// not taking uni-switch's models: left alone, nothing read
		matters = false
		fresh()
		usage.Store(0)
		if CodexUsedUp(t.Context()) || usage.Load() != 0 {
			t.Fatalf("%s, Codex not on uni-switch: read %d times", c.name, usage.Load())
		}
		// taking them: the app's hold is read, and known after
		matters = true
		fresh()
		if !CodexUsedUp(t.Context()) {
			t.Fatalf("%s: CodexUsedUp = false on a held account", c.name)
		}
		if usage.Load() != 1 {
			t.Fatalf("%s: read %d times, want 1", c.name, usage.Load())
		}
		if !CodexUsedUpKnown() {
			t.Fatalf("%s: CodexUsedUpKnown = false after the read", c.name)
		}
		// the Usage page still reads nothing of it
		if u := LoginUsage(t.Context(), "codex"); len(u) != 0 {
			t.Fatalf("%s: LoginUsage = %v, want nothing", c.name, u)
		}
	}

	// a sign-in about to expire is left to Codex: not refreshed, not
	// written, the hold unknown
	auth := filepath.Join(home, ".codex", "auth.json")
	var a map[string]any
	b, _ := os.ReadFile(auth)
	json.Unmarshal(b, &a)
	a["tokens"].(map[string]any)["access_token"] = fakeJWT(map[string]any{"exp": float64(time.Now().Add(-time.Minute).Unix())})
	writeFile(t, auth, a)
	before, _ := os.ReadFile(auth)
	fresh()
	usage.Store(0)
	if CodexUsedUp(t.Context()) {
		t.Fatal("expired sign-in: CodexUsedUp = true")
	}
	after, _ := os.ReadFile(auth)
	if refresh.Load() != 0 || usage.Load() != 0 || !bytes.Equal(before, after) {
		t.Fatalf("expired sign-in left alone: refreshed %d, read %d, auth.json rewritten %v", refresh.Load(), usage.Load(), !bytes.Equal(before, after))
	}
}
