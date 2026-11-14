package provider

import (
	"context"
	"testing"
	"time"
)

// #959: an answer given from the kept copy while the accounts are read
// again says so, for the page to ask again once the new reading lands; the
// read done, it no longer does.
func TestSubscriptionUsageReadingWhileStale(t *testing.T) {
	claudeHome(t)
	c := &subscriptionUsageCache
	c.Lock()
	oldAt, oldData, oldAsked, oldPending := c.at, c.data, c.asked, c.pending
	c.at, c.asked, c.pending = time.Now(), false, nil
	c.data = []SubscriptionQuota{{Provider: "codex", User: "a@example.com", Windows: []QuotaWindow{{Name: "5 hours", Used: 40}}}}
	c.Unlock()
	t.Cleanup(func() {
		c.Lock()
		c.at, c.data, c.asked, c.pending = oldAt, oldData, oldAsked, oldPending
		c.Unlock()
	})

	SubscriptionUsage(context.Background())
	if SubscriptionUsageReading() {
		t.Fatal("a fresh copy, with nothing read again, said to be reading")
	}

	held := make(chan struct{})
	c.Lock()
	c.pending = held
	c.Unlock()
	if !SubscriptionUsageReading() {
		t.Fatal("a read under way not told")
	}
	c.Lock()
	c.pending = nil
	c.Unlock()

	// stale: the kept copy comes back at once and a read starts behind it,
	// told until it has landed
	c.Lock()
	c.at = time.Now().Add(-2 * time.Minute)
	c.Unlock()
	got := SubscriptionUsage(context.Background())
	if len(got) != 1 || got[0].Windows[0].Used != 40 {
		t.Fatalf("the kept copy not answered at once: %+v", got)
	}
	c.Lock()
	p := c.pending
	c.Unlock()
	if p != nil {
		<-p
	}
	if SubscriptionUsageReading() {
		t.Fatal("still said to be reading after the read landed")
	}
}
