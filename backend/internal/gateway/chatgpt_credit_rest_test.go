package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// PaulT on Discord: a ChatGPT account on the credits its plan gives, the
// credits run out, answered 402 and was asked again every half hour
// (creditRest), each time refused the same. It now sits out until its own
// window renews when its reading has one full, else five hours, and a
// reading finding the window renewed has it back. Another vendor's 402
// rests as it did. No real body of ChatGPT's 402 is known, so the test
// sends none: the status alone is what it rests by.
func TestChatGPTOutOfCreditsRestsLong(t *testing.T) {
	fresh(t)
	s := New()
	old, oldStale := allowances, staleAllowance
	t.Cleanup(func() { allowances, staleAllowance = old, oldStale })
	staleAllowance = func(string, string) {} // nothing is read again here
	var reading provider.Allowance
	allowances = func(agent string) map[string]provider.Allowance {
		if agent != "codex" || reading == nil {
			return nil
		}
		return map[string]provider.Allowance{"me@example.com": reading}
	}
	acct := candidate{p: provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "me@example.com"}}, model: "gpt-5.5", rest: "codex@me@example.com"}
	near := func(d, want time.Duration) bool { return d > want-time.Minute && d <= want }

	// nothing read: five hours, not thirty minutes
	r := s.restAfter(acct, http.StatusPaymentRequired, http.Header{}, nil)
	if r.Why != failCredit || !near(time.Until(r.Until), chatgptCreditRest) {
		t.Fatalf("unread: rests %v (%s, by %s), want %v", time.Until(r.Until).Round(time.Second), r.Why, r.By, chatgptCreditRest)
	}
	// its window renewed (a reset spent, a reading): back at once
	renewed("codex", "me@example.com")
	if r, ok := restOf(acct.restKey()); ok {
		t.Fatalf("unread: still resting after its window renewed: %v by %s", time.Until(r.Until).Round(time.Second), r.By)
	}

	// its five hours read full, back in three: till then, the reading
	// saying it holds credits all the same (they are what ran out)
	back := time.Now().Add(3 * time.Hour)
	reading = provider.Allowance{
		{Used: 100, Resets: back, Span: 5 * time.Hour},
		{Used: 40, Resets: time.Now().Add(4 * 24 * time.Hour), Span: 7 * 24 * time.Hour},
	}
	r = s.restAfter(acct, http.StatusPaymentRequired, http.Header{}, nil)
	if r.By != "window" || !near(time.Until(r.Until), 3*time.Hour) {
		t.Fatalf("window full: rests %v by %s, want 3h by window", time.Until(r.Until).Round(time.Second), r.By)
	}
	if _, ok := restOf(acct.restKey()); !ok {
		t.Fatal("not resting")
	}
	// a reading finding the window renewed has it back
	renewed("codex", "me@example.com")
	if r, ok := restOf(acct.restKey()); ok {
		t.Fatalf("still resting after its window renewed: %v by %s", time.Until(r.Until).Round(time.Second), r.By)
	}

	// a key out of credit, any other vendor's account: as before
	key := candidate{p: provider.Provider{ID: "relay", Key: "k"}, model: "m", rest: "relay"}
	if r := s.restAfter(key, http.StatusPaymentRequired, http.Header{}, nil); r.By != "credit" || !near(time.Until(r.Until), creditRest) {
		t.Fatalf("key: rests %v by %s, want %v", time.Until(r.Until).Round(time.Second), r.By, creditRest)
	}
	other := candidate{p: provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "me@example.com"}}, model: "claude-x", rest: "claude@me@example.com"}
	if r := s.restAfter(other, http.StatusPaymentRequired, http.Header{}, nil); r.By != "credit" || !near(time.Until(r.Until), creditRest) {
		t.Fatalf("claude account: rests %v by %s, want %v", time.Until(r.Until).Round(time.Second), r.By, creditRest)
	}
	renewed("claude", "me@example.com")
	if _, ok := restOf(other.restKey()); !ok {
		t.Fatal("another vendor's credit rest lifted by its window renewing")
	}
}
