package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// 0xAncientTwo on X: the Claude subscription was signed out again and again.
// A saved account switched in to Claude Code itself has its directory
// removed; the runs waiting there for their next turn hold its refresh
// token, now Claude Code's, and are let go, so none refreshes it under
// Claude Code. Runs of other accounts, and runs in Claude Code's own home
// (which read its sign-in afresh), wait on.
func TestClaudeHandOverLetsGoOfTheAccountsRuns(t *testing.T) {
	b := newSubscriptionBridge()
	run := func(owner string) *subscriptionRun {
		r := &subscriptionRun{bridge: b, pending: map[string]chan mcpToolResult{}}
		r.owner.Store(&owner)
		b.mu.Lock()
		b.idle[owner] = r
		r.idleKey = owner
		b.mu.Unlock()
		return r
	}
	moved := run("claude\x00B@example.com")
	other := run("claude\x00a@example.com")
	own := run("claude\x00b@example.com\x00" + ownHome)

	provider.ClaudeHandedOver("b@example.com")

	if !moved.closed {
		t.Fatal("a run in the handed-over account's directory waits on")
	}
	if other.closed || own.closed {
		t.Fatal("another account's run, or Claude Code's own, was let go")
	}
	b.mu.Lock()
	_, kept := b.idle["claude\x00B@example.com"]
	n := len(b.idle)
	b.mu.Unlock()
	if kept || n != 2 {
		t.Fatalf("idle runs after the hand-over: %d, moved kept %v", n, kept)
	}
}

// #4532: a run in a turn as its account is handed over isn't kept for the
// next turn when this one ends: kept, it waited up to idleLongest in the
// removed directory with the refresh token that is Claude Code's now.
// Another account's run in a turn is kept as before.
func TestClaudeHandOverEndsTheAccountsBusyRuns(t *testing.T) {
	b := newSubscriptionBridge()
	run := func(owner string) *subscriptionRun {
		r := &subscriptionRun{bridge: b, token: owner, pending: map[string]chan mcpToolResult{}}
		r.owner.Store(&owner)
		b.mu.Lock()
		b.runs[owner] = r // in a turn: a run, not waiting
		b.mu.Unlock()
		return r
	}
	moved := run("claude\x00b@example.com")
	other := run("claude\x00a@example.com")

	provider.ClaudeHandedOver("b@example.com")

	if b.keepIdle(moved, "next", "") || !moved.closed {
		t.Fatal("a run of the handed-over account was kept for its next turn")
	}
	if !b.keepIdle(other, "other", "") || other.closed {
		t.Fatal("another account's run in a turn was let go")
	}
	b.mu.Lock()
	_, kept := b.idle["next"]
	b.mu.Unlock()
	if kept {
		t.Fatal("the handed-over account's run waits for a next turn")
	}
}
