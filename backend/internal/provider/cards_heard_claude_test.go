package provider

import (
	"testing"
	"time"
)

// #2206 (SuInk): a Claude account's allowance heard only as Claude Code
// answered (NoteClaudeLimits) is on the cards another uni-switch is told
// (CachedCards), not "nothing read on that uni-switch yet". A reading started
// after it stands in its place, and one started before it doesn't.
func TestCachedCardsTellHeardClaudeLimits(t *testing.T) {
	claudeUsageCards(t, "", nil) // a@example.com signed in, nothing read
	reset := func() {
		claudeUsage.Lock()
		claudeUsage.m = nil
		claudeUsage.Unlock()
		subscriptionUsageCache.Lock()
		subscriptionUsageCache.data = nil
		subscriptionUsageCache.Unlock()
	}
	reset()
	t.Cleanup(reset)
	used := func() float64 {
		t.Helper()
		q := cardOf(CachedCards(time.Now()), "claude", "a@example.com")
		if q == nil {
			t.Fatal("no Claude card, though Claude Code told its allowance")
		}
		if len(q.Windows) == 0 || q.Error != "" || q.ReadAt == nil {
			t.Fatalf("Claude card without the windows told: %+v", q)
		}
		return q.Windows[0].Used
	}

	// a reading started before (its seq taken), done after the header
	before := quotaReadSequence.Add(1)
	resetAt := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	NoteClaudeLimits("a@example.com", []ClaudeLimit{{Kind: "five_hour", Used: 0.42, ResetsAt: resetAt.Unix()}})
	if got := used(); got != 42 {
		t.Fatalf("heard reading: used %v, want 42", got)
	}
	readAt := time.Now()
	old := SubscriptionQuota{Provider: "claude", User: "a@example.com", ReadAt: &readAt, Windows: []QuotaWindow{{Name: "5 hours", Used: 10, ResetsAt: &resetAt}}, readSeq: before}
	loginUsageCache.Lock()
	loginUsageCache.m = map[string]loginUsageEntry{"claude/a@example.com": {at: time.Now(), q: old, read: old}}
	loginUsageCache.Unlock()
	if got := used(); got != 42 {
		t.Fatalf("a reading started before the header took its place: used %v, want 42", got)
	}

	// a reading started after it stands
	newer := old
	newer.Windows = []QuotaWindow{{Name: "5 hours", Used: 55, ResetsAt: &resetAt}}
	newer.readSeq = quotaReadSequence.Add(1)
	loginUsageCache.Lock()
	loginUsageCache.m = map[string]loginUsageEntry{"claude/a@example.com": {at: time.Now(), q: newer, read: newer}}
	loginUsageCache.Unlock()
	if got := used(); got != 55 {
		t.Fatalf("a reading started after the header: used %v, want 55", got)
	}

	// and the next header after that
	NoteClaudeLimits("a@example.com", []ClaudeLimit{{Kind: "five_hour", Used: 0.6, ResetsAt: resetAt.Unix()}})
	if got := used(); got != 60 {
		t.Fatalf("a later header: used %v, want 60", got)
	}
}
