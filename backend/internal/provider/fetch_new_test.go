package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// An account signed in while uni-switch runs gets its vendor's list the next
// time the providers are listed, not only after Refresh (#204: WorkBuddy's
// models incomplete when first added, all of them after Refresh; Codex is
// the built-in signed in here). One whose list couldn't be had isn't asked
// again on each listing.
func TestFetchNewAccount(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	newFetches.Lock()
	newFetches.m = map[string]time.Time{}
	newFetches.Unlock()

	var asked atomic.Int32
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/models" {
			w.WriteHeader(404)
			return
		}
		asked.Add(1)
		if down.Load() {
			w.WriteHeader(502)
			return
		}
		w.Write(codexListOf("gpt-6-luna", "gpt-6-astra", "gpt-6-sol"))
	}))
	defer srv.Close()
	codexListedAt(t, srv.URL)

	ids := func() map[string]bool {
		p, err := Find("codex")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, m := range p.Available() {
			out[m.ID] = true
		}
		return out
	}

	// nothing signed in yet: nothing asked, and nothing missing
	if FetchNew(time.Second) {
		t.Fatal("missing a list with no account")
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("asked %d times with no account", n)
	}

	// Codex signs itself in while uni-switch runs
	codexSignedIn(t, home)
	if ids()["gpt-6-luna"] {
		t.Fatal("the vendor's model listed before its list was fetched")
	}
	if FetchNew(5 * time.Second) {
		t.Fatal("a list fetched said missing")
	}
	got := ids()
	if !got["gpt-6-luna"] || !got["gpt-6-astra"] || !got["gpt-6-sol"] {
		t.Fatalf("after FetchNew: %v", got)
	}
	FetchNew(5 * time.Second)
	if n := asked.Load(); n != 1 {
		t.Fatalf("a fetched list asked for again: %d", n)
	}

	// a list that can't be had is asked for once, and again only later
	if err := catalog.SaveLive("codex", "", nil); err != nil {
		t.Fatal(err)
	}
	newFetches.Lock()
	newFetches.m = map[string]time.Time{}
	newFetches.Unlock()
	down.Store(true)
	// missing both times: when it failed, and while it waits to ask again,
	// so the terminal and uni-switch serve try once more later (#1789)
	if !FetchNew(5 * time.Second) {
		t.Fatal("a list that failed not said missing")
	}
	if !FetchNew(5 * time.Second) {
		t.Fatal("a list waiting to be asked again not said missing")
	}
	if n := asked.Load(); n != 2 {
		t.Fatalf("a failing vendor asked %d times, want once more", n-1)
	}
	oldRetry := newFetchRetry
	newFetchRetry = 0
	defer func() { newFetchRetry = oldRetry }()
	down.Store(false)
	if FetchNew(5 * time.Second) {
		t.Fatal("missing after the list came")
	}
	if n := asked.Load(); n != 3 || !ids()["gpt-6-luna"] {
		t.Fatalf("not asked again after the retry time: %d %v", n, ids())
	}
}

// codexSignedIn signs Codex in under home, with no list of its own kept:
// a built-in account whose list uni-switch fetches.
func codexSignedIn(t *testing.T, home string) {
	t.Helper()
	exp := float64(time.Now().Add(24 * time.Hour).Unix())
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token":      fakeJWT(map[string]any{"email": "me@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-1"}}),
			"access_token":  fakeJWT(map[string]any{"exp": exp}),
			"refresh_token": "r", "account_id": "acct-1",
		},
	})
	forgetAccountCaches()
}

// codexListedAt points Codex's models list at a test's server, and keeps
// the machine's own Codex CLI out of the version it is asked with.
func codexListedAt(t *testing.T, base string) {
	t.Helper()
	oldBase, oldExe := CodexBase, codexExecutable
	CodexBase = base + "/backend-api/codex"
	codexExecutable = func() string { return "" }
	t.Cleanup(func() { CodexBase, codexExecutable = oldBase, oldExe })
}

// codexListOf is ChatGPT's answer listing the models named.
func codexListOf(ids ...string) []byte {
	var ms []map[string]any
	for i, id := range ids {
		ms = append(ms, map[string]any{"slug": id, "display_name": id, "visibility": "list", "priority": i + 1})
	}
	b, _ := json.Marshal(map[string]any{"models": ms})
	return b
}
