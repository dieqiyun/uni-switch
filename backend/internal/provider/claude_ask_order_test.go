package provider

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// An ask that comes while a Claude reading is under way is answered by a
// new reading, even when the ask's clock reads no later than the reading's
// start: Windows' clock ticks every ~15ms, and a wall clock can be set back
// while a read runs (#1228). askOnSameTick asks, then stores the ask's time
// as of before, as such a clock would have read it.
func askOnSameTick(before time.Time) {
	AskClaudeUsage()
	claudeAsked.Store(before.UnixNano())
}

// blockClaudeUsage has /usage wait for release, telling running once it is.
func blockClaudeUsage(t *testing.T) (running chan struct{}, release chan struct{}) {
	t.Helper()
	running, release = make(chan struct{}, 1), make(chan struct{})
	UsageClaudeVia(func(context.Context, string) (string, error) {
		select {
		case running <- struct{}{}:
		default:
		}
		<-release
		return "Current session: 40% used", nil
	})
	return running, release
}

func waitRunning(t *testing.T, running chan struct{}) {
	t.Helper()
	select {
	case <-running:
	case <-time.After(5 * time.Second):
		t.Fatal("/usage wasn't run")
	}
}

func TestLoginUsageClaudeAskedOnTheReadsTick(t *testing.T) {
	claudeUsageCards(t, "Current session: 40% used", nil)
	running, release := blockClaudeUsage(t)
	AskClaudeUsage()
	before := time.Now()
	done := make(chan struct{})
	go func() { defer close(done); LoginUsage(context.Background(), "claude") }()
	waitRunning(t, running)
	askOnSameTick(before)
	close(release)
	<-done
	loginUsageCache.Lock()
	_, kept := loginUsageCache.m["claude/a@example.com"]
	loginUsageCache.Unlock()
	if kept {
		t.Fatal("a reading from before the ask was kept")
	}
}

func TestSubscriptionUsageClaudeAskedOnTheReadsTick(t *testing.T) {
	claudeUsageCards(t, "Current session: 40% used", nil)
	c := &subscriptionUsageCache
	c.Lock()
	oldAt, oldData, oldAsked := c.at, c.data, c.asked
	c.at, c.data, c.asked = time.Time{}, nil, false
	c.Unlock()
	t.Cleanup(func() {
		c.Lock()
		c.at, c.data, c.asked = oldAt, oldData, oldAsked
		c.Unlock()
	})
	running, release := blockClaudeUsage(t)
	AskClaudeUsage()
	before := time.Now()
	done := make(chan struct{})
	go func() { defer close(done); SubscriptionUsage(context.Background()) }()
	waitRunning(t, running)
	askOnSameTick(before)
	close(release)
	<-done
	for SubscriptionUsageReading() {
		time.Sleep(time.Millisecond)
	}
	c.Lock()
	at := c.at
	c.Unlock()
	if !at.IsZero() {
		t.Fatalf("cards read before the ask were kept as fresh, as of %v", at)
	}
}

func TestClaudeWindowsAskedOnTheRunsTick(t *testing.T) {
	var out atomic.Value
	out.Store("Current session: 40% used · resets " + sessionReset() + "\n")
	runs := fakeClaudeUsage(t, &out, nil)
	ctx := context.Background()
	AskClaudeUsage()
	if _, err := claudeWindows(ctx, "a@x", true); err != nil || runs.Load() != 1 {
		t.Fatalf("asked: %v %d", err, runs.Load())
	}
	// a minute on (past the floor, short of the unasked wait), the user
	// asks again, on the clock's tick the run above started on
	aMinuteAgo := func() time.Time {
		claudeUsage.Lock()
		defer claudeUsage.Unlock()
		e := claudeUsage.m["a@x"]
		e.tried = e.tried.Add(-time.Minute)
		claudeUsage.m["a@x"] = e
		return e.tried
	}
	askOnSameTick(aMinuteAgo())
	if _, err := claudeWindows(ctx, "a@x", true); err != nil || runs.Load() != 2 {
		t.Fatalf("the ask wasn't answered: %v, %d runs", err, runs.Load())
	}
	// answered: no run again until asked, past the floor too
	aMinuteAgo()
	if _, err := claudeWindows(ctx, "a@x", true); err != nil || runs.Load() != 2 {
		t.Fatalf("ran again unasked: %v, %d runs", err, runs.Load())
	}
}
