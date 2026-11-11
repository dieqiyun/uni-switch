package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Spending credits first is a fourth way, one of the four an account has:
// setting it clears last, setting last or off clears it, and on clears
// both.
func TestCodexCreditsFirstMode(t *testing.T) {
	signIn(t)
	if CodexCreditsMode("me@example.com") != CreditsOn || CodexCreditsFirst("me@example.com") {
		t.Fatal("first before it was set")
	}
	if err := SetCodexCreditsMode("me@example.com", CreditsLast); err != nil {
		t.Fatal(err)
	}
	if err := SetCodexCreditsMode(" Me@Example.com ", CreditsFirst); err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	if !slices.Equal(s.CodexCreditsFirst, []string{"me@example.com"}) || len(s.CodexCreditsLast)+len(s.CodexNoCredits) != 0 {
		t.Fatalf("kept first %v, last %v, off %v", s.CodexCreditsFirst, s.CodexCreditsLast, s.CodexNoCredits)
	}
	if !CodexCreditsFirst("ME@example.com") || CodexCreditsLast("me@example.com") || CodexCreditsMode("me@example.com") != CreditsFirst || CodexCreditsFirst("other@example.com") {
		t.Fatal("read back wrong")
	}
	for _, mode := range []string{CreditsLast, CreditsOff, CreditsOn} {
		if err := SetCodexCreditsMode("me@example.com", CreditsFirst); err != nil {
			t.Fatal(err)
		}
		if err := SetCodexCreditsMode("me@example.com", mode); err != nil {
			t.Fatal(err)
		}
		if CodexCreditsFirst("me@example.com") || CodexCreditsMode("me@example.com") != mode {
			t.Fatalf("%s kept first: %v", mode, settings.Load().CodexCreditsFirst)
		}
	}
}

// Codex signed in to an account whose week is used up but that holds
// credits is signed in to the next account with room, as by default
// (TestCodexSwitchedWhenUsedUp), unless the account is set to spend its
// credits first (jojonas_xyz on X): then it stays on it while it holds
// them, and moves on once they are gone.
func TestCodexStaysOnAnAccountSpendingCreditsFirst(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	codexSignIn(t, home, "main@example.com", "r-main") // the one Codex is on
	rememberLogins(true)
	if err := SetLoginOn("codex", "spare@example.com", true); err != nil {
		t.Fatal(err)
	}
	used := map[string]float64{"acct-main@example.com": 100, "acct-1": 100, "acct-spare@example.com": 20}
	credits := true
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": used[r.Header.Get("chatgpt-account-id")], "limit_window_seconds": 604800}}}
		if r.Header.Get("chatgpt-account-id") == "acct-main@example.com" && credits {
			body["credits"] = map[string]any{"has_credits": true, "unlimited": false, "balance": "120"}
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	t.Cleanup(func() {
		codexApp.Lock()
		codexApp.switched, codexApp.spent = false, false
		codexApp.Unlock()
	})
	next := func() (string, bool) {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		_, to, _, ok := NextLogin(context.Background(), "codex")
		return to, ok
	}
	// by default, credits or not, it moves
	if to, ok := next(); !ok || to != "spare@example.com" {
		t.Fatalf("default: to %q, %v", to, ok)
	}
	// set to spend them first, it stays
	if err := SetCodexCreditsMode("main@example.com", CreditsFirst); err != nil {
		t.Fatal(err)
	}
	if to, ok := next(); ok {
		t.Fatalf("first: moved to %q", to)
	}
	// its credits gone, it moves on
	credits = false
	if to, ok := next(); !ok || to != "spare@example.com" {
		t.Fatalf("first, no credits left: to %q, %v", to, ok)
	}
}
