package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// tongzhenbao on X: a Codex account set to spend a reset by itself when
// its week runs out (that switch alone on), its week used up, holding a
// reset that runs out after the week starts again, and let spend its
// credits. ChatGPT answers it on its credits, so nothing refuses it as out
// of its allowance, and the reset was never spent while ~10k credits were.
// The reset is spent before the turn goes on the credits, once a week;
// with the week's switch off, the turn goes on the credits as before.
func TestCodexResetBeforeCredits(t *testing.T) {
	reporter := func(t *testing.T, b *resetBackend, user, acct string) {
		t.Helper()
		// answers all the same, on its credits; the reset held runs out
		// 20 days on, past the week's own reset 3 days on
		b.credits[acct] = true
		if err := provider.SetCodexAutoResetCase(user, provider.ResetCaseWeek, true); err != nil {
			t.Fatal(err)
		}
		if w, e := provider.CodexAutoResetWhen(user); !w || e {
			t.Fatalf("set week only: week %v expiring %v", w, e)
		}
	}
	t.Run("relayed", func(t *testing.T) {
		codexSignedIn(t)
		b := newResetBackend(t, "acct-1")
		creditsUsage(t, map[string]float64{"me@example.com": 100}, "me@example.com")
		reporter(t, b, "Me@example.com", "acct-1")
		srv := New()
		code, body := resetPost(t, srv)
		if tried, spent := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" || spent != "acct-1" {
			t.Fatalf("%d %s tried %q spent %q", code, body, tried, spent)
		}
		if got := resetsTraced(srv); len(got) != 1 || got[0] != "me@example.com: 2 windows started again" {
			t.Fatalf("traced %v", got)
		}
		if calls := srv.Recent(); len(calls) == 0 || !strings.Contains(calls[0].Fallback, "before its credits") {
			t.Fatalf("request log %+v", calls)
		}
		// used up again in the same week: the credits go on, no second reset
		b.mu.Lock()
		b.out["acct-1"], b.week["acct-1"], b.held["acct-1"] = true, 100, 1
		b.mu.Unlock()
		if code, _ := resetPost(t, srv); code != 200 {
			t.Fatalf("second time in the week: %d", code)
		}
		if _, spent := b.seen(); spent != "acct-1" {
			t.Fatalf("second time in the week spent %q", spent)
		}
	})
	t.Run("routed", func(t *testing.T) {
		codexSignedIn(t, "spare@example.com")
		b := newResetBackend(t, "acct-1", "acct-2")
		b.held["acct-2"] = 0
		// spare used up with no credits: benched, never asked
		creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 100}, "me@example.com")
		reporter(t, b, "me@example.com", "acct-1")
		srv := New()
		code, body := resetPost(t, srv)
		if tried, spent := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" || spent != "acct-1" {
			t.Fatalf("%d %s tried %q spent %q", code, body, tried, spent)
		}
		if got := resetsTraced(srv); len(got) != 1 || got[0] != "me@example.com: 2 windows started again" {
			t.Fatalf("traced %v", got)
		}
	})
	t.Run("week's switch off", func(t *testing.T) {
		codexSignedIn(t)
		b := newResetBackend(t, "acct-1")
		b.credits["acct-1"] = true
		creditsUsage(t, map[string]float64{"me@example.com": 100}, "me@example.com")
		if err := provider.SetCodexAutoResetCase("me@example.com", provider.ResetCaseExpiring, true); err != nil {
			t.Fatal(err)
		}
		code, _ := resetPost(t, New())
		if tried, spent := b.seen(); code != 200 || tried != "acct-1" || spent != "" {
			t.Fatalf("%d tried %q spent %q", code, tried, spent)
		}
	})
	t.Run("week not used up", func(t *testing.T) {
		codexSignedIn(t)
		b := newResetBackend(t)
		b.week["acct-1"], b.held["acct-1"], b.credits["acct-1"] = 60, 1, true
		creditsUsage(t, map[string]float64{"me@example.com": 60}, "me@example.com")
		if err := provider.SetCodexAutoResetCase("me@example.com", provider.ResetCaseWeek, true); err != nil {
			t.Fatal(err)
		}
		code, _ := resetPost(t, New())
		if tried, spent := b.seen(); code != 200 || tried != "acct-1" || spent != "" {
			t.Fatalf("%d tried %q spent %q", code, tried, spent)
		}
	})
}
