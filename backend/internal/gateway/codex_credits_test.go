package gateway

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A ChatGPT account at 100% of its five hours that holds credits answers
// on them (the backend answers it): by default it is asked first as the
// order says and spends them; set not to spend its credits, it is held as
// used up and the next account takes the request, the trace saying why.
func TestCodexNoCreditsMovesOn(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t) // every account answers: the first on its credits
	capUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20})
	if err := provider.SetRouting("codex", provider.Ordered); err != nil {
		t.Fatal(err)
	}
	code, body := resetPost(t, New())
	if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" {
		t.Fatalf("credits spent: %d %s tried %s", code, body, tried)
	}

	if err := provider.SetCodexCredits("Me@Example.com", false); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.tried = nil
	b.mu.Unlock()
	srv := New()
	code, body = resetPost(t, srv)
	if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-2" {
		t.Fatalf("credits not spent: %d %s tried %s", code, body, tried)
	}
	var left []string
	for _, r := range srv.Trace(t.Context(), 0, 0).Routes {
		for _, w := range r.Left {
			if w.NoCredits && w.Capped == 100 && w.CapBack != nil {
				left = append(left, w.Who)
			}
		}
	}
	if len(left) != 1 || left[0] != "me@example.com" {
		t.Fatalf("traced as held for its credits: %v", left)
	}
}

// Every account held, as none may spend its credits or one is at its cap:
// the client is refused as when all are used up (429), told why in the
// words of what holds each, and how to go on; nothing reaches the vendor.
func TestCodexNoCreditsEveryAccount(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t)
	capUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 100})
	for _, u := range []string{"me@example.com", "spare@example.com"} {
		if err := provider.SetCodexCredits(u, false); err != nil {
			t.Fatal(err)
		}
	}
	srv := New()
	code, body := resetPost(t, srv)
	if tried, _ := b.seen(); code != 429 || tried != "" {
		t.Fatalf("%d tried %q: %s", code, tried, body)
	}
	for _, want := range []string{"rate_limit_error", "set in uni-switch not to spend its credits", "me@example.com", "spare@example.com", "if an account holds credits, let it spend them", "quota credits"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in %s", want, body)
		}
	}
	if strings.Contains(body, "usage cap reached") {
		t.Errorf("told as a cap: %s", body)
	}
	// the request's record says the same as the client was told
	if c := srv.Recent()[0]; c.Status != 429 || c.Error != "every account held: set not to spend its credits" {
		t.Errorf("recorded %d %q", c.Status, c.Error)
	}

	// one held at its cap, the other for its credits: both told
	if err := provider.SetAccountCap("codex", "me@example.com", 70); err != nil {
		t.Fatal(err)
	}
	code, body = resetPost(t, srv)
	if tried, _ := b.seen(); code != 429 || tried != "" {
		t.Fatalf("%d tried %q: %s", code, tried, body)
	}
	for _, want := range []string{"held by uni-switch", "past its 70% cap", "(spare@example.com) has used up a usage window and is set not to spend its credits", "account-cap", "if an account holds credits, let it spend them", "quota credits"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in %s", want, body)
		}
	}
	if c := srv.Recent()[0]; c.Status != 429 || c.Error != "every account held: at its usage cap or set not to spend its credits" {
		t.Errorf("recorded %d %q", c.Status, c.Error)
	}
}

// Codex's one account, its week used up and credits left, set not to
// spend them and to spend its resets by itself: relayed as it came it
// would answer on its credits; routed, it is held, and with nobody else
// left a reset is spent first and the turn goes through on the windows
// started again — the trace says so. Without leave to spend resets, it is
// refused, and nothing is spent.
func TestCodexNoCreditsSpendsAReset(t *testing.T) {
	codexSignedIn(t)
	b := newResetBackend(t)
	b.week["acct-1"], b.held["acct-1"] = 100, 1 // answers all the same: credits
	capUsage(t, map[string]float64{"me@example.com": 100})
	if err := provider.SetCodexCredits("me@example.com", false); err != nil {
		t.Fatal(err)
	}
	srv := New()
	code, body := resetPost(t, srv)
	if tried, spent := b.seen(); code != 429 || !strings.Contains(body, "not to spend its credits") || tried != "" || spent != "" {
		t.Fatalf("no auto-reset: %d %s tried %q spent %q", code, body, tried, spent)
	}

	if err := provider.SetCodexAutoReset("me@example.com", true); err != nil {
		t.Fatal(err)
	}
	code, body = resetPost(t, srv)
	if tried, spent := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" || spent != "acct-1" {
		t.Fatalf("auto-reset: %d %s tried %q spent %q", code, body, tried, spent)
	}
	if got := resetsTraced(srv); len(got) != 1 || got[0] != "me@example.com: 2 windows started again" {
		t.Fatalf("traced %v", got)
	}
	// the account it was spent on is in the trace's order, tried, not
	// left out
	r := srv.Trace(t.Context(), 0, 0).Routes[0]
	if len(r.Order) != 1 || r.Order[0].Who != "me@example.com" || r.Order[0].Capped != 0 || len(r.Left) != 0 {
		t.Fatalf("order %+v left %+v", r.Order, r.Left)
	}
}

// The account Codex is signed in to is out of its allowance, its credits
// spent or none, and the only other is held as it won't spend its credits,
// its week used up, with leave to spend its resets: once the first is
// refused, the held one spends a reset and answers.
func TestCodexNoCreditsResetAfterTheLastRefusal(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t, "acct-1")
	b.held["acct-1"] = 0
	b.week["acct-2"], b.held["acct-2"] = 100, 1
	capUsage(t, map[string]float64{"me@example.com": 60, "spare@example.com": 100})
	if err := provider.SetCodexCredits("spare@example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetCodexAutoReset("spare@example.com", true); err != nil {
		t.Fatal(err)
	}
	srv := New()
	code, body := resetPost(t, srv)
	if tried, spent := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1,acct-2" || spent != "acct-2" {
		t.Fatalf("%d %s tried %q spent %q", code, body, tried, spent)
	}
	if got := resetsTraced(srv); len(got) != 1 || got[0] != "spare@example.com: 2 windows started again" {
		t.Fatalf("traced %v", got)
	}
}

// creditsUsage is capUsage for Codex accounts whose five hours and week
// are each at used's share, those named in credits holding credits as
// /wham/usage says (provider.QuotaWindow.Credits).
func creditsUsage(t *testing.T, used map[string]float64, credits ...string) {
	t.Helper()
	old := allowances
	t.Cleanup(func() { allowances = old })
	allowances = func(string) map[string]provider.Allowance {
		out := map[string]provider.Allowance{}
		for u, n := range used {
			c := slices.Contains(credits, u)
			out[u] = provider.Allowance{
				{Used: n, Resets: time.Now().Add(2 * time.Hour), Span: 5 * time.Hour, Credits: c},
				{Used: n, Resets: time.Now().Add(3 * 24 * time.Hour), Span: 7 * 24 * time.Hour, Credits: c},
			}
		}
		return out
	}
}

// triesOf is each try of srv's routes, as account: why it failed, how
// long it rests and why, and whether it was asked again onto its credits.
func triesOf(srv *Server) []Try {
	var out []Try
	for _, r := range srv.Trace(context.Background(), 0, 0).Routes {
		out = append(out, r.Tries...)
	}
	return out
}

// Zhenzhen on Discord: a Codex account that holds credits used up its
// allowance mid-session, and the turn that ran it out was refused
// (usage_limit_reached) while the next session answered on its credits.
// The refused turn is asked again of the same account at once, and
// answers: the session doesn't end. Codex's one account goes through the
// relay, two or more through routing; each asks again once a request.
func TestCodexCreditsAskedAgainAtTheEdge(t *testing.T) {
	t.Run("relayed", func(t *testing.T) {
		codexSignedIn(t)
		b := newResetBackend(t, "acct-1")
		b.credits["acct-1"], b.edge["acct-1"] = true, 1
		creditsUsage(t, map[string]float64{"me@example.com": 100}, "me@example.com")
		srv := New()
		code, body := resetPost(t, srv)
		if tried, spent := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1,acct-1" || spent != "" {
			t.Fatalf("%d %s tried %s spent %s", code, body, tried, spent)
		}
		if ts := triesOf(srv); len(ts) == 0 || !ts[0].Credits {
			t.Fatalf("not traced as asked again onto its credits: %+v", ts)
		}
		// refused again: told as it came, not asked a third time
		b.mu.Lock()
		b.tried, b.edge["acct-1"] = nil, 2
		b.mu.Unlock()
		if code, _ := resetPost(t, srv); code != 429 {
			t.Fatalf("refused twice: %d", code)
		}
		if tried, _ := b.seen(); tried != "acct-1,acct-1" {
			t.Fatalf("refused twice, tried %s", tried)
		}
	})
	t.Run("routed", func(t *testing.T) {
		codexSignedIn(t, "spare@example.com")
		b := newResetBackend(t, "acct-1", "acct-2")
		b.credits["acct-1"], b.edge["acct-1"] = true, 1
		creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 100}, "me@example.com")
		if err := provider.SetRouting("codex", provider.Ordered); err != nil {
			t.Fatal(err)
		}
		srv := New()
		code, body := resetPost(t, srv)
		if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1,acct-1" {
			t.Fatalf("%d %s tried %s", code, body, tried)
		}
		ts := triesOf(srv)
		if len(ts) != 2 || !ts[0].Credits || ts[0].Rest != nil || ts[1].Status != 200 {
			t.Fatalf("traced %+v", ts)
		}
	})
	t.Run("set not to spend them", func(t *testing.T) {
		codexSignedIn(t)
		b := newResetBackend(t, "acct-1")
		b.credits["acct-1"], b.edge["acct-1"] = true, 1
		creditsUsage(t, map[string]float64{"me@example.com": 100}, "me@example.com")
		if err := provider.SetCodexCredits("me@example.com", false); err != nil {
			t.Fatal(err)
		}
		// held as used up (TestCodexNoCreditsEveryAccount): never sent,
		// so never asked again
		code, _ := resetPost(t, New())
		if tried, _ := b.seen(); code != 429 || tried != "" {
			t.Fatalf("%d tried %s", code, tried)
		}
	})
}

// 豆子 on Discord: a Codex account out of its allowance that holds credits
// still answers on them, so it isn't benched until its window renews —
// neither when ChatGPT refuses it as out of its allowance (it rests a
// minute, and its usage is read again) nor when it fails otherwise with
// its windows full (it rests as any account that failed does). One with
// no credits still rests until its allowance is back.
func TestCodexCreditsNoLongRest(t *testing.T) {
	t.Run("refused as out", func(t *testing.T) {
		codexSignedIn(t, "spare@example.com")
		b := newResetBackend(t, "acct-1")
		b.credits["acct-1"], b.edge["acct-1"] = true, 5
		creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20}, "me@example.com")
		if err := provider.SetRouting("codex", provider.Ordered); err != nil {
			t.Fatal(err)
		}
		srv := New()
		code, body := resetPost(t, srv)
		if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1,acct-1,acct-2" {
			t.Fatalf("%d %s tried %s", code, body, tried)
		}
		ts := triesOf(srv)
		if len(ts) != 3 || !ts[0].Credits || ts[1].Rest == nil {
			t.Fatalf("traced %+v", ts)
		}
		if r := ts[1].Rest; r.By != "credits" || r.Why != failQuota || time.Until(r.Until) > fallbackCooldown {
			t.Fatalf("rests %s for %s until %v", r.By, r.Why, r.Until)
		}
	})
	t.Run("failed otherwise", func(t *testing.T) {
		codexSignedIn(t, "spare@example.com")
		b := newResetBackend(t)
		b.fails["acct-1"] = 500
		creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20}, "me@example.com")
		if err := provider.SetRouting("codex", provider.Ordered); err != nil {
			t.Fatal(err)
		}
		srv := New()
		if code, body := resetPost(t, srv); code != 200 || !strings.Contains(body, "pong") {
			t.Fatalf("%d %s", code, body)
		}
		ts := triesOf(srv)
		if len(ts) == 0 || ts[0].Rest == nil || ts[0].Rest.By != "backoff" || time.Until(ts[0].Rest.Until) > longestRetry {
			t.Fatalf("traced %+v", ts)
		}
	})
	t.Run("no credits", func(t *testing.T) {
		codexSignedIn(t, "spare@example.com")
		b := newResetBackend(t, "acct-1")
		creditsUsage(t, map[string]float64{"me@example.com": 100, "spare@example.com": 20})
		if err := provider.SetRouting("codex", provider.Ordered); err != nil {
			t.Fatal(err)
		}
		srv := New()
		if code, _ := resetPost(t, srv); code != 200 {
			t.Fatalf("%d", code)
		}
		if tried, _ := b.seen(); tried != "acct-1,acct-2" {
			t.Fatalf("tried %s", tried)
		}
		ts := triesOf(srv)
		if len(ts) == 0 || ts[0].Credits || ts[0].Rest == nil || time.Until(ts[0].Rest.Until) < time.Hour {
			t.Fatalf("traced %+v", ts)
		}
	})
}

// Every account all but used up: the one that goes on on its credits is
// asked before one with nothing to go on with, though it has used more,
// and the trace says it holds them (豆子 on Discord).
func TestCodexCreditsFirstOfTheSpent(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t, "acct-1")
	b.credits["acct-2"] = true
	creditsUsage(t, map[string]float64{"me@example.com": 99, "spare@example.com": 100}, "spare@example.com")
	srv := New()
	code, body := resetPost(t, srv)
	if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-2" {
		t.Fatalf("%d %s tried %s", code, body, tried)
	}
	rs := srv.Trace(t.Context(), 0, 0).Routes
	if len(rs) == 0 || len(rs[0].Order) != 2 || rs[0].Order[0].Who != "spare@example.com" || !rs[0].Order[0].Credits || rs[0].Order[1].Credits {
		t.Fatalf("order %+v", rs)
	}
}
