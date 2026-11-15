package provider

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A reset spent from the Usage page is known to routing by the time the
// page hears it was spent (#1491, isdou): the account's windows read again
// as started over, not left out of the allowances, where routing counted it
// "not known" and sent requests to the next account, and the Routing page
// said so, until a later request's reading came back. That holds too when a
// reading begun before the reset is still out: it can't count the reset, so
// another is asked for.
func TestResetAllowanceKnownAtOnce(t *testing.T) {
	signIn(t)
	week := func(used float64) map[string]SubscriptionQuota {
		resets := time.Now().Add(100 * time.Hour)
		return map[string]SubscriptionQuota{
			"me@example.com":    {Windows: []QuotaWindow{{Span: 7 * 24 * time.Hour, Used: used, ResetsAt: &resets}}},
			"other@example.com": {Windows: []QuotaWindow{{Span: 7 * 24 * time.Hour, Used: 40, ResetsAt: &resets}}},
		}
	}
	var spent atomic.Bool
	hold, entered := make(chan struct{}), make(chan struct{})
	var held atomic.Bool
	LoginUsageVia(func(context.Context, string) map[string]SubscriptionQuota {
		if held.CompareAndSwap(true, false) {
			// the reading out when the reset is spent: it read the
			// windows before
			close(entered)
			<-hold
			return week(100)
		}
		if spent.Load() {
			return week(0)
		}
		return week(100)
	})
	clear := func() {
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale, usedCache.renewed, usedCache.seen = nil, nil, nil
		usedCache.Unlock()
	}
	clear()
	t.Cleanup(func() { LoginUsageVia(nil); clear() })
	oldWait := firstWait
	firstWait = 5 * time.Second
	t.Cleanup(func() { firstWait = oldWait })

	// the allowances routing has now, without asking for more
	used := func() (float64, bool) {
		usedCache.Lock()
		a, ok := usedCache.m["codex"]["me@example.com"]
		usedCache.Unlock()
		if !ok || len(a) == 0 {
			return 0, false
		}
		return a[0].Used, true
	}
	// a reading still out when the test ends is waited for
	t.Cleanup(func() {
		usedCache.Lock()
		done := usedCache.loading["codex"]
		usedCache.Unlock()
		if done != nil {
			<-done
		}
	})
	Allowances("codex")
	if u, ok := used(); !ok || u != 100 {
		t.Fatalf("before the reset: %v %v", u, ok)
	}

	for _, inFlight := range []bool{false, true} {
		spent.Store(false)
		forgetAllowance("codex", "other@example.com") // read again at the next ask
		if inFlight {
			held.Store(true)
			Allowances("codex") // a reading goes out, and waits
			<-entered
		}
		// what UseCodexReset does once the vendor says the windows started
		// again
		spent.Store(true)
		StaleAllowance("codex", "me@example.com")
		renewedNow("codex", "me@example.com")
		if inFlight {
			close(hold)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		known := AwaitAllowance(ctx, "codex", "Me@example.com")
		cancel()
		if !known {
			t.Fatalf("in flight %v: the account isn't known once the reset is spent", inFlight)
		}
		if u, ok := used(); !ok || u != 0 {
			t.Fatalf("in flight %v: routing reads %v used (known %v), want 0", inFlight, u, ok)
		}
	}
}
