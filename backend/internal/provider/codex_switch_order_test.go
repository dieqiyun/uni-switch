package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// In order, Codex stays signed in to the account it is on at 98% of its
// allowance, as the gateway still sends that one requests first, and moves
// on once it is used up (#530); Smart moves it at 98%, as before. Kept on
// the first (KeepLogin), it stays however used up that is, and moves on
// again once that is off (#524).
func TestCodexLoginOrderedAndKept(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	if err := SetLoginOn("codex", "spare@example.com", true); err != nil {
		t.Fatal(err)
	}
	used := map[string]float64{"acct-work@example.com": 98, "acct-1": 0, "acct-spare@example.com": 20}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window":   map[string]any{"used_percent": 0, "limit_window_seconds": 18000},
			"secondary_window": map[string]any{"used_percent": used[r.Header.Get("chatgpt-account-id")], "limit_window_seconds": 604800}}})
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	switched := func() string {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		to, err := SwitchWhenSpent(context.Background(), "codex")
		if err != nil {
			t.Fatal(err)
		}
		return to
	}
	active := func() string {
		t.Helper()
		for _, l := range Logins("codex") {
			if l.Active {
				return l.User
			}
		}
		return ""
	}
	if active() != "work@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// in order: at 98% it stays, used up it moves on
	if err := SetRouting("codex", Ordered); err != nil {
		t.Fatal(err)
	}
	if to := switched(); to != "" {
		t.Fatalf("in order, moved off an account at 98%%, to %s", to)
	}
	used["acct-work@example.com"] = 100
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("in order, used up: moved to %q", to)
	}
	// renewed, back on the first
	used["acct-work@example.com"] = 0
	if to := switched(); to != "work@example.com" {
		t.Fatalf("back to %q", to)
	}

	// smart: at 98% it moves on, as before
	if err := SetRouting("codex", ""); err != nil {
		t.Fatal(err)
	}
	used["acct-work@example.com"] = 98
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("smart at 98%%: moved to %q", to)
	}
	used["acct-work@example.com"] = 0
	if to := switched(); to != "work@example.com" {
		t.Fatalf("back to %q", to)
	}

	// kept on the first: it stays, used up or not; the setting outlives a
	// save of the provider's picks
	if err := SetKeepLogin("codex", true); err != nil {
		t.Fatal(err)
	}
	if err := SetRouting("codex", ""); err != nil {
		t.Fatal(err)
	}
	if p, err := Find("codex"); err != nil || !p.KeepLogin {
		t.Fatalf("kept on the first: %+v %v", p, err)
	}
	used["acct-work@example.com"] = 100
	if to := switched(); to != "" {
		t.Fatalf("kept on the first, moved to %s", to)
	}
	if active() != "work@example.com" {
		t.Fatalf("kept on the first, signed in to %s", active())
	}
	// let go: it moves on again
	if err := SetKeepLogin("codex", false); err != nil {
		t.Fatal(err)
	}
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("let go: moved to %q", to)
	}
	// kept on the first again: back on it at once, used up as it is
	if err := SetKeepLogin("codex", true); err != nil {
		t.Fatal(err)
	}
	if to := switched(); to != "work@example.com" {
		t.Fatalf("kept on the first again: moved to %q", to)
	}
	if to := switched(); to != "" {
		t.Fatalf("kept on the first again, then moved to %s", to)
	}

	// only Codex's and Claude Code's sign-ins are moved
	if err := SetKeepLogin("nonesuch", true); err == nil {
		t.Fatal("kept a provider that isn't there")
	}
}
