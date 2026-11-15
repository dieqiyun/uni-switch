package provider

import (
	"context"
	"testing"
	"time"
)

// swapWaits says whether swap, run while a call through the stand-in it
// replaces is held, waits for that call: Allowances reads in the
// background and returns without waiting, so a reading it began outlasts
// the test that set the stand-in, and the test's swap back raced it (the
// ubuntu race shard of run 37942751214, TestClaudeAccountBackOnceItsWindowRenews).
func swapWaits(t *testing.T, set func(block func()), call func(), swap func()) {
	t.Helper()
	running, release := make(chan struct{}), make(chan struct{})
	set(func() {
		close(running)
		<-release
	})
	called := make(chan struct{})
	go func() { defer close(called); call() }()
	select {
	case <-running:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the stand-in wasn't called")
	}
	swapped := make(chan struct{})
	go func() { defer close(swapped); swap() }()
	select {
	case <-swapped:
		close(release)
		<-called
		t.Fatal("swapped while a call was still running through the stand-in it replaced")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for _, c := range []chan struct{}{called, swapped} {
		select {
		case <-c:
		case <-time.After(5 * time.Second):
			t.Fatal("the swap didn't go on once the call was done")
		}
	}
}

func TestLoginUsageViaWaitsForReadingsThroughTheOld(t *testing.T) {
	t.Cleanup(func() { LoginUsageVia(nil) })
	swapWaits(t, func(block func()) {
		LoginUsageVia(func(context.Context, string) map[string]SubscriptionQuota {
			block()
			return map[string]SubscriptionQuota{}
		})
	}, func() { LoginUsage(context.Background(), "swap-waits") }, func() { LoginUsageVia(nil) })
}

func TestUsageClaudeViaWaitsForRunsThroughTheOld(t *testing.T) {
	claudeCLIUsage.RLock()
	old := claudeCLIUsage.f
	claudeCLIUsage.RUnlock()
	t.Cleanup(func() { UsageClaudeVia(old) })
	swapWaits(t, func(block func()) {
		UsageClaudeVia(func(context.Context, string) (string, error) {
			block()
			return "", nil
		})
	}, func() { _, _ = readClaudeUsage(context.Background(), "") }, func() { UsageClaudeVia(nil) })
}
