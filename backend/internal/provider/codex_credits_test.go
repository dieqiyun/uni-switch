package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The credits a ChatGPT account holds besides its windows are its balance
// (#571), shown when it has some to spend and they are not unlimited.
// Routing is told the account goes on past its windows on them (onCredits:
// every window's Credits, and the allowance's OnCredits) when it holds
// some or they are unlimited, as the Codex app reads has_credits; a
// balance said and spent is none.
func TestCodexCreditsBalance(t *testing.T) {
	for _, c := range []struct {
		name      string
		credits   any
		want      string
		onCredits bool
	}{
		{"none said", nil, "", false},
		{"held", map[string]any{"has_credits": true, "unlimited": false, "balance": "1234.5"}, "1234.5 credits", true},
		{"a few", map[string]any{"has_credits": true, "unlimited": false, "balance": "42"}, "42 credits", true},
		{"none held", map[string]any{"has_credits": false, "unlimited": false, "balance": "0"}, "", false},
		{"spent", map[string]any{"has_credits": true, "unlimited": false, "balance": "0"}, "", false},
		{"unlimited", map[string]any{"has_credits": true, "unlimited": true, "balance": ""}, "", true},
		{"unreadable", map[string]any{"has_credits": true, "balance": "lots"}, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
					"primary_window": map[string]any{"used_percent": 100, "limit_window_seconds": 18000}}}
				if c.credits != nil {
					body["credits"] = c.credits
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer fake.Close()
			old := CodexBase
			CodexBase = fake.URL + "/backend-api/codex"
			defer func() { CodexBase = old }()
			plan, windows, _, credits, _, err := codexWindows(context.Background(), "tok", "acct-1")
			if err != nil || plan != "plus" || len(windows) != 1 {
				t.Fatalf("plan %q, windows %v, err %v", plan, windows, err)
			}
			if credits != c.want {
				t.Errorf("credits %q, want %q", credits, c.want)
			}
			if windows[0].Credits != c.onCredits || allowanceOf(windows, time.Now()).OnCredits() != c.onCredits {
				t.Errorf("on credits %v / %v, want %v", windows[0].Credits, allowanceOf(windows, time.Now()).OnCredits(), c.onCredits)
			}
		})
	}
}

// Whether an account spends its credits is kept by account, whatever its
// case: on unless turned off. Turned off, a Codex account is held at 100%
// of its windows; a cap holds it sooner, and nothing holds another
// vendor's account for its credits.
func TestCodexCreditsSwitch(t *testing.T) {
	signIn(t)
	if !CodexCredits("me@example.com") {
		t.Fatal("off before it was turned off")
	}
	if err := SetCodexCredits(" Me@Example.com ", false); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().CodexNoCredits; !slices.Equal(got, []string{"me@example.com"}) {
		t.Fatalf("kept %v", got)
	}
	if CodexCredits("ME@example.com") || !CodexCredits("other@example.com") {
		t.Fatal("read back wrong")
	}
	p := Provider{ID: "codex", AccountCaps: map[string]int{"capped@example.com": 70}}
	for _, c := range []struct {
		agent, user string
		share       int
		noCredits   bool
	}{
		{"codex", "me@example.com", 100, true},
		{"codex", "other@example.com", 0, false},
		{"claude", "me@example.com", 0, false},
		{"codex", "capped@example.com", 70, false},
	} {
		if share, no := HoldCaps(p, c.agent, c.user).Of("5 hours"); share != c.share || no != c.noCredits {
			t.Errorf("%s %s: held at %d (credits %v)", c.agent, c.user, share, no)
		}
	}
	if err := SetCodexCredits("me@example.com", true); err != nil {
		t.Fatal(err)
	}
	if !CodexCredits("me@example.com") || len(settings.Load().CodexNoCredits) != 0 {
		t.Fatal("still off")
	}
}

// Spending credits last is a third way, kept by account whatever its
// case: set, the account still spends them (nothing holds it), and off
// or on clears it; a mode not known is refused, nothing written.
func TestCodexCreditsLastMode(t *testing.T) {
	signIn(t)
	if CodexCreditsMode("me@example.com") != CreditsOn || CodexCreditsLast("me@example.com") {
		t.Fatal("last before it was set")
	}
	if err := SetCodexCreditsMode(" Me@Example.com ", CreditsLast); err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	if !slices.Equal(s.CodexCreditsLast, []string{"me@example.com"}) || len(s.CodexNoCredits) != 0 {
		t.Fatalf("kept last %v, off %v", s.CodexCreditsLast, s.CodexNoCredits)
	}
	if !CodexCreditsLast("ME@example.com") || !CodexCredits("me@example.com") || CodexCreditsMode("me@example.com") != CreditsLast || CodexCreditsLast("other@example.com") {
		t.Fatal("read back wrong")
	}
	if share, _ := HoldCaps(Provider{ID: "codex"}, "codex", "me@example.com").Of("5 hours"); share != 0 {
		t.Fatalf("held at %d: last still spends them", share)
	}
	if err := SetCodexCreditsMode("me@example.com", "sometimes"); err == nil || !CodexCreditsLast("me@example.com") {
		t.Fatalf("a mode not known: %v", err)
	}
	if err := SetCodexCredits("me@example.com", false); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); len(s.CodexCreditsLast) != 0 || CodexCreditsMode("me@example.com") != CreditsOff {
		t.Fatalf("off kept last: %v", s.CodexCreditsLast)
	}
	if err := SetCodexCreditsMode("me@example.com", CreditsLast); err != nil {
		t.Fatal(err)
	}
	if err := SetCodexCredits("me@example.com", true); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); len(s.CodexCreditsLast)+len(s.CodexNoCredits) != 0 || CodexCreditsMode("me@example.com") != CreditsOn {
		t.Fatalf("on kept last: %v", s.CodexCreditsLast)
	}
}

// A reset spent starts an account's windows again: what was read of them
// before is forgotten, so what holds the account at a share of them (its
// cap, its credits not spent) holds it no more, and they are read again
// at once; the other accounts' readings stay.
func TestRenewedAccountForgetsItsAllowance(t *testing.T) {
	signIn(t)
	usedCache.Lock()
	usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
	usedCache.stale = nil
	usedCache.m["codex"] = map[string]Allowance{
		"me@example.com":    {{Used: 100, Span: 5 * time.Hour}},
		"spare@example.com": {{Used: 10, Span: 5 * time.Hour}},
	}
	usedCache.at["codex"] = time.Now()
	handed := usedCache.m["codex"]
	usedCache.Unlock()
	t.Cleanup(func() {
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale = nil
		usedCache.Unlock()
	})
	renewedNow("codex", "Me@Example.com")
	usedCache.Lock()
	m, at := usedCache.m["codex"], usedCache.at["codex"]
	usedCache.Unlock()
	if _, ok := m["me@example.com"]; ok {
		t.Fatalf("still read as before: %v", m["me@example.com"])
	}
	if _, ok := m["spare@example.com"]; !ok || !at.IsZero() {
		t.Fatalf("spare kept %v, read again at once %v", ok, at.IsZero())
	}
	if _, ok := handed["me@example.com"]; !ok {
		t.Fatal("the map handed out before was changed")
	}
}
