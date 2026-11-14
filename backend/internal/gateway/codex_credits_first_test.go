package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// jojonas_xyz on X: an account's allowance ran out and uni-switch went on to
// another of the user's accounts, where they may have wanted its credits
// spent. Routed Smart, the default, an account all but used up waits
// behind every one with room, credits or not; set to spend its credits
// first, a window it uses up doesn't move it: it keeps the place an
// account with room has, low (90%) or used up, and the trace says it is
// so. Set back on (in its place), it waits behind again.
func TestCodexCreditsFirstKeepsItsPlace(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t) // every account answers: the first on its credits
	creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20}, "me@example.com")
	again := func(want string) *Server {
		t.Helper()
		b.mu.Lock()
		b.tried = nil
		b.mu.Unlock()
		srv := New()
		code, body := resetPost(t, srv)
		if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != want {
			t.Fatalf("%d %s tried %q, want %q", code, body, tried, want)
		}
		return srv
	}
	again("acct-2") // Smart: the one with room first

	if err := provider.SetCodexCreditsMode("Me@Example.com", provider.CreditsFirst); err != nil {
		t.Fatal(err)
	}
	srv := again("acct-1")
	rs := srv.Trace(t.Context(), 0, 0).Routes
	if len(rs) == 0 || len(rs[0].Order) != 2 || rs[0].Order[0].Who != "me@example.com" || !rs[0].Order[0].CreditsFirst ||
		rs[0].Order[1].Who != "spare@example.com" || rs[0].Order[1].CreditsFirst {
		t.Fatalf("order %+v", rs)
	}

	// low, not used up yet: it stays too
	creditsUsage(t, map[string]float64{"me@example.com": 95, "spare@example.com": 20}, "me@example.com")
	again("acct-1")

	// with no credits to go on with, it waits behind as before
	creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20})
	again("acct-2")

	creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20}, "me@example.com")
	if err := provider.SetCodexCredits("me@example.com", true); err != nil {
		t.Fatal(err)
	}
	again("acct-2")
}

// Routed Until used up, the pool goes on from an account once a window of
// it is used up; set to spend its credits first, it stays on it.
func TestCodexCreditsFirstDrainStays(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t)
	creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20}, "me@example.com")
	if err := provider.SetRouting("codex", provider.Drain); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetCodexCreditsMode("me@example.com", provider.CreditsFirst); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.tried = nil
	b.mu.Unlock()
	code, body := resetPost(t, New())
	if tried, _ := b.seen(); code != 200 || tried != "acct-1" {
		t.Fatalf("%d %s tried %q", code, body, tried)
	}
}
