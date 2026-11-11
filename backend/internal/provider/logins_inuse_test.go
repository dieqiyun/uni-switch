package provider

import (
	"testing"
	"time"
)

// noRoom is inUseOf's room for a caller with no allowances to weigh: the
// account in use stays whichever it has left, as it did before the menu
// bar's card was made to follow the gateway.
func noRoom(string) (bool, bool) { return false, false }

// loginRoom weighs an account against the share the gateway's routing counts
// it spent at, reading the allowances the gateway routes by. inUseOf relies
// on it to decide whether the account in use still has room, so it must
// agree with the gateway on which accounts are out: a full one is spent, one
// with room is not, and one never read is unknown — never taken for spent, so
// the card never jumps off an account whose allowance it hasn't seen.
func TestLoginRoom(t *testing.T) {
	signIn(t)
	// Inject the allowances as the gateway would see them: Codex's signed-in
	// account at the vendor's edge, a spare with room, and an account uni-switch
	// has no reading for. Marked fresh so Allowances answers from them rather
	// than refetching over a test's fake home.
	usedCache.Lock()
	if usedCache.m == nil {
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
	}
	usedCache.m["codex"] = map[string]Allowance{
		"me@example.com":    {{Used: 100, Span: 5 * time.Hour}},
		"spare@example.com": {{Used: 10, Span: 5 * time.Hour}},
	}
	usedCache.at["codex"] = time.Now()
	usedCache.Unlock()
	t.Cleanup(func() {
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale = nil
		usedCache.Unlock()
	})

	room := loginRoom("codex")
	if known, spent := room("me@example.com"); !known || !spent {
		t.Errorf("me: known=%v spent=%v, want both: a full account is spent", known, spent)
	}
	if known, spent := room("spare@example.com"); !known || spent {
		t.Errorf("spare: known=%v spent=%v, want known and not spent", known, spent)
	}
	if known, spent := room("nobody@example.com"); known || spent {
		t.Errorf("nobody: known=%v spent=%v, want unknown: an unread allowance is never spent", known, spent)
	}
}
