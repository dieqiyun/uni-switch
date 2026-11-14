package provider

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// plugins#349: a careful provider asks when each account's usage was
// last read well and whether the last try failed (UsageRead), and has it
// read again before a request (ReadUsageNow). A failed try keeps the last
// good reading's time, so its age still counts.
func TestUsageReadAndReadAgain(t *testing.T) {
	signIn(t)
	resets := time.Now().Add(3 * time.Hour)
	var meFails atomic.Bool
	LoginUsageVia(func(context.Context, string) map[string]SubscriptionQuota {
		me := SubscriptionQuota{Windows: []QuotaWindow{{Span: 5 * time.Hour, Used: 40, ResetsAt: &resets}}}
		if meFails.Load() {
			me = SubscriptionQuota{Error: "usage endpoint said 503"}
		}
		return map[string]SubscriptionQuota{
			"me@example.com":    me,
			"other@example.com": {Error: "usage endpoint said 401"},
		}
	})
	clear := func() {
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale, usedCache.renewed, usedCache.seen, usedCache.read = nil, nil, nil, nil
		usedCache.Unlock()
	}
	clear()
	t.Cleanup(func() {
		usedCache.Lock()
		done := usedCache.loading["codex"]
		usedCache.Unlock()
		if done != nil {
			<-done
		}
		LoginUsageVia(nil)
		clear()
	})
	if _, _, ok := UsageRead("codex", "me@example.com"); ok {
		t.Fatal("known before any read")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	before := time.Now()
	if !ReadUsageNow(ctx, "codex", "me@example.com") {
		t.Fatal("no reading came back")
	}
	at, failed, ok := UsageRead("codex", "ME@example.com")
	if !ok || failed || at.Before(before) {
		t.Fatalf("me: at %v (asked %v) failed %v ok %v", at, before, failed, ok)
	}
	if _, failed, ok := UsageRead("codex", "other@example.com"); !ok || !failed {
		t.Fatalf("other: failed %v ok %v", failed, ok)
	}

	// read again: a newer good reading
	good := at
	if !ReadUsageNow(ctx, "codex", "me@example.com") {
		t.Fatal("no second reading")
	}
	if at, failed, _ := UsageRead("codex", "me@example.com"); failed || !at.After(good) {
		t.Fatalf("read again: at %v, was %v, failed %v", at, good, failed)
	}
	good, _, _ = UsageRead("codex", "me@example.com")

	// it fails: failed, the last good reading's time kept
	meFails.Store(true)
	if !ReadUsageNow(ctx, "codex", "me@example.com") {
		t.Fatal("no third reading")
	}
	if at, failed, _ := UsageRead("codex", "me@example.com"); !failed || !at.Equal(good) {
		t.Fatalf("failed read: at %v, good %v, failed %v", at, good, failed)
	}
}

// An account within the given points of a window's cap, or past it, is
// near it; one further off, a window renewed since, or no cap is not.
func TestNearCap(t *testing.T) {
	now := time.Now()
	al := func(used float64, resets time.Time) Allowance {
		return Allowance{{Used: used, Resets: resets, Span: 5 * time.Hour}}
	}
	later, earlier := now.Add(time.Hour), now.Add(-time.Minute)
	caps := WindowCaps{All: 90}
	for _, c := range []struct {
		name string
		a    Allowance
		caps WindowCaps
		want bool
	}{
		{"within five points", al(86, later), caps, true},
		{"past it", al(95, later), caps, true},
		{"further off", al(80, later), caps, false},
		{"renewed since", al(88, earlier), caps, false},
		{"no cap", al(99, later), WindowCaps{}, false},
	} {
		if got := c.a.NearCap("gpt-5.5", c.caps, 5, now); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// Stop at the caps and Check usage near the cap are kept with the
// provider, off until set, and a save of the rest keeps them.
func TestCapStopAndCarefulKept(t *testing.T) {
	isolate(t)
	if err := Save(Provider{ID: "plan", Name: "Plan", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("plan"); p.CapStop || p.CapCareful {
		t.Fatalf("on by default: %+v", p)
	}
	if err := SetCapStop("plan", true); err != nil {
		t.Fatal(err)
	}
	if err := SetCapCareful("plan", true); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("plan")
	if !p.CapStop || !p.CapCareful {
		t.Fatalf("not kept: stop %v careful %v", p.CapStop, p.CapCareful)
	}
	p.Name = "Plan 2"
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("plan"); !p.CapStop || !p.CapCareful || p.Name != "Plan 2" {
		t.Fatalf("after a save: %+v", p)
	}
	if err := SetCapStop("plan", false); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("plan"); p.CapStop || !p.CapCareful {
		t.Fatalf("stop off: stop %v careful %v", p.CapStop, p.CapCareful)
	}
}
