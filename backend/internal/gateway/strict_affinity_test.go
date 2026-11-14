package gateway

import (
	"net/http/httptest"
	"strings"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1797 (miniers): a conversation set to stay strictly with the account
// that answered it never moves to another. "strict" is spelled out here,
// not provider.AffinityStrict, for the test to say what the code before it
// did: it took "strict" for auto.

// strictSetup signs Codex in to two accounts, routed in order, its
// conversations staying strictly, with no conversation remembered.
func strictSetup(t *testing.T) *resetBackend {
	t.Helper()
	codexSignedIn(t, "spare@example.com")
	sticks.Lock()
	sticks.m, sticks.opening, sticks.from = map[string]stick{}, map[string]stick{}, ""
	sticks.Unlock()
	t.Cleanup(func() {
		sticks.Lock()
		sticks.m, sticks.opening, sticks.from = map[string]stick{}, map[string]stick{}, ""
		sticks.Unlock()
	})
	b := newResetBackend(t)
	if err := provider.SetRouting("codex", provider.Ordered); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAffinity("codex", "strict"); err != nil {
		t.Fatal(err)
	}
	if p, err := provider.Find("codex"); err != nil || p.Affinity != "strict" {
		t.Fatalf("saved as %+v, %v", p, err)
	}
	return b
}

// strictPost posts a turn of the Codex conversation session to srv, and
// says which accounts the backend was asked it by.
func strictPost(t *testing.T, srv *Server, b *resetBackend, session string) (int, string, string) {
	t.Helper()
	b.mu.Lock()
	b.tried = nil
	b.mu.Unlock()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":"ping `+session+`"}`))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	req.Header.Set("session_id", session)
	srv.Handler().ServeHTTP(rec, req)
	tried, _ := b.seen()
	return rec.Code, rec.Body.String(), tried
}

// lastWhy is why the routing trace says the last request stayed or not.
func lastWhy(t *testing.T, srv *Server) string {
	rs := srv.Trace(t.Context(), 0, 0).Routes
	if len(rs) == 0 || rs[len(rs)-1].Affinity == nil {
		return ""
	}
	return rs[len(rs)-1].Affinity.Why
}

// The account a strict conversation is on runs out (429): its error goes
// to the agent, and nobody else is asked it — where "session" moves it to
// the next account, as it did. A new conversation goes to the next.
func TestStrictAffinityPassesTheErrorOn(t *testing.T) {
	b := strictSetup(t)
	srv := New()
	if code, body, tried := strictPost(t, srv, b, "s1"); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" {
		t.Fatalf("first turn: %d %s tried %s", code, body, tried)
	}
	b.mu.Lock()
	b.out["acct-1"] = true
	b.mu.Unlock()
	code, body, tried := strictPost(t, srv, b, "s1")
	if code != 429 || !strings.Contains(body, "usage limit") || tried != "acct-1" {
		t.Fatalf("strict, its account out: %d %s tried %s", code, body, tried)
	}
	if why := lastWhy(t, srv); why != "strict" {
		t.Fatalf("traced as %q", why)
	}
	// resting now, it is still the one asked
	if code, _, tried := strictPost(t, srv, b, "s1"); code != 429 || tried != "acct-1" {
		t.Fatalf("strict, its account resting: %d tried %s", code, tried)
	}
	if code, body, tried := strictPost(t, srv, b, "s2"); code != 200 || !strings.Contains(body, "pong") || tried != "acct-2" {
		t.Fatalf("a new conversation: %d %s tried %s", code, body, tried)
	}

	if err := provider.SetAffinity("codex", provider.AffinitySession); err != nil {
		t.Fatal(err)
	}
	if code, body, tried := strictPost(t, srv, b, "s1"); code != 200 || !strings.Contains(body, "pong") || tried != "acct-2" {
		t.Fatalf("session moves it on: %d %s tried %s", code, body, tried)
	}
}

// The usage cap set on an account keeps new conversations off it, while a
// strict conversation already on it goes on there past the cap.
func TestStrictAffinityGoesPastTheCap(t *testing.T) {
	b := strictSetup(t)
	capUsage(t, map[string]float64{"me@example.com": 60, "spare@example.com": 20})
	if err := provider.SetAccountCap("codex", "me@example.com", 70); err != nil {
		t.Fatal(err)
	}
	srv := New()
	if code, _, tried := strictPost(t, srv, b, "s1"); code != 200 || tried != "acct-1" {
		t.Fatalf("below the cap: %d tried %s", code, tried)
	}
	capUsage(t, map[string]float64{"me@example.com": 75, "spare@example.com": 20})
	if code, body, tried := strictPost(t, srv, b, "s1"); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" {
		t.Fatalf("strict, past the cap: %d %s tried %s", code, body, tried)
	}
	if why := lastWhy(t, srv); why != "strict" {
		t.Fatalf("traced as %q", why)
	}
	if code, _, tried := strictPost(t, srv, b, "s2"); code != 200 || tried != "acct-2" {
		t.Fatalf("a new conversation past the cap: %d tried %s", code, tried)
	}
}

// An account set not to spend its credits, at 100%: a strict conversation
// on it is refused, as held, rather than moved; nothing reaches the
// vendor. A new conversation goes to the next account.
func TestStrictAffinityHeldForCreditsIsRefused(t *testing.T) {
	b := strictSetup(t)
	capUsage(t, map[string]float64{"me@example.com": 50, "spare@example.com": 20})
	srv := New()
	if code, _, tried := strictPost(t, srv, b, "s1"); code != 200 || tried != "acct-1" {
		t.Fatalf("first turn: %d tried %s", code, tried)
	}
	capUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20})
	if err := provider.SetCodexCredits("me@example.com", false); err != nil {
		t.Fatal(err)
	}
	code, body, tried := strictPost(t, srv, b, "s1")
	if code != 429 || tried != "" || !strings.Contains(body, "me@example.com") || !strings.Contains(body, "Stays: Strict") {
		t.Fatalf("strict, held for its credits: %d %s tried %q", code, body, tried)
	}
	if code, _, tried := strictPost(t, srv, b, "s2"); code != 200 || tried != "acct-2" {
		t.Fatalf("a new conversation: %d tried %s", code, tried)
	}
}
