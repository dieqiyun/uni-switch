package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// A model the vendor adds to a signed-in account's plan reaches uni-switch
// without Refresh: the gateway's uni-switch asks for the account's list again
// once it is accountListAge old (cliff_26651 on Discord: GPT-6.1 Terra,
// served to ChatGPT accounts on 10-26, missing from Codex through uni-switch,
// whose list of the account was fetched on 10-12). A list still fresh
// isn't asked for, and one that can't be had keeps the last.
func TestAccountListFollowsTheVendor(t *testing.T) {
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
	var added, down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/models" {
			w.WriteHeader(404)
			return
		}
		asked.Add(1)
		switch {
		case down.Load():
			w.WriteHeader(502)
		case added.Load():
			w.Write(codexListOf("gpt-6.1-sol", "gpt-6-astra", "gpt-6.1-terra", "gpt-6-luna"))
		default:
			w.Write(codexListOf("gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna"))
		}
	}))
	defer srv.Close()
	codexListedAt(t, srv.URL)
	codexSignedIn(t, home)

	served := func() []string {
		p, err := Find("codex")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range p.Available() {
			out = append(out, m.ID)
		}
		return out
	}
	FetchNew(5 * time.Second)
	if n := asked.Load(); n != 1 || !slices.Contains(served(), "gpt-6-astra") {
		t.Fatalf("FetchNew: asked %d, serves %v", n, served())
	}

	// OpenAI adds a model; the list uni-switch has is a minute old
	added.Store(true)
	ctx := context.Background()
	if got := relistAccounts(ctx, time.Now()); len(got) != 0 || asked.Load() != 1 {
		t.Fatalf("a fresh list asked for again: %v (%d asks)", got, asked.Load())
	}
	later := time.Now().Add(accountListAge + time.Minute)
	if got := relistAccounts(ctx, later); !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("a list %s old not asked for again: %v", accountListAge+time.Minute, got)
	}
	if !slices.Contains(served(), "gpt-6.1-terra") {
		t.Fatalf("the model the vendor added isn't served: %v", served())
	}

	// the vendor can't be reached: the list there was stays
	down.Store(true)
	n := asked.Load()
	relistAccounts(ctx, later.Add(accountListAge+time.Minute))
	if asked.Load() != n+1 {
		t.Fatal("the vendor wasn't asked")
	}
	if !slices.Contains(served(), "gpt-6.1-terra") || !slices.Contains(served(), "gpt-6-astra") {
		t.Fatalf("a failed fetch lost the list: %v", served())
	}

	// switched off, it isn't asked
	down.Store(false)
	if err := SetOff("codex", true); err != nil {
		t.Fatal(err)
	}
	n = asked.Load()
	if got := relistAccounts(ctx, later.Add(3*accountListAge)); len(got) != 0 || asked.Load() != n {
		t.Fatalf("an account switched off asked: %v", got)
	}
}

// The gateway's loop does it by itself, and stops with its context.
func TestKeepAccountsListedAsks(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Write(codexListOf("gpt-6.1-sol", "gpt-6.1-terra"))
	}))
	defer srv.Close()
	codexListedAt(t, srv.URL)
	codexSignedIn(t, home)
	FetchNew(5 * time.Second)
	if asked.Load() != 1 {
		t.Fatalf("FetchNew asked %d times", asked.Load())
	}

	oldFirst, oldEvery, oldAge := accountRelistFirst, accountRelistEvery, accountListAge
	accountRelistFirst, accountRelistEvery, accountListAge = time.Millisecond, time.Millisecond, 0
	defer func() { accountRelistFirst, accountRelistEvery, accountListAge = oldFirst, oldEvery, oldAge }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		KeepAccountsListed(ctx)
	}()
	deadline := time.Now().Add(30 * time.Second)
	for asked.Load() < 3 && time.Now().Before(deadline) {
		<-time.After(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := asked.Load(); n < 3 {
		t.Fatalf("the loop asked the vendor %d times in 30s", n-1)
	}
}
