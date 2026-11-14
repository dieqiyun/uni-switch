package provider

import (
	"sync/atomic"
	"testing"
	"time"
)

// A key's background reading measures a window started again from the last
// successful reading, not from a later failure or the current read. A
// reset that passed between those successful readings is therefore told
// on the successful read after it, even when a failed read came between.
func TestKeyAllowanceElapsedResetStartsAgain(t *testing.T) {
	keyLimitsHome(t)
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	full := weekUsed(t, "800", reset)
	var down atomic.Bool
	srv, asked := sub2apiServer(t, func() ([]byte, int) {
		if down.Load() {
			return nil, 503
		}
		return full, 200
	})
	p := sub2apiKey(t, srv)
	if err := Save(p); err != nil {
		t.Fatal(err)
	}

	startedHooks.Lock()
	hooks := startedHooks.fs
	startedHooks.Unlock()
	t.Cleanup(func() {
		startedHooks.Lock()
		startedHooks.fs = hooks
		startedHooks.Unlock()
	})
	id := KeyAllowanceID(p)
	if id == "" {
		t.Fatal("the key has no allowance id")
	}
	started := make(chan string, 8)
	OnStarted(func(agent, user string) {
		if user == id {
			select {
			case started <- agent + "/" + user:
			default:
			}
		}
	})
	read := func() {
		keyAllowances.Lock()
		gen := keyAllowances.gen
		keyAllowances.Unlock()
		readKeyAllowance(p, id, gen)
	}
	// Seed a successful reading whose reset passed before the next
	// successful reading, without making the test wait for wall time.
	wasAt := time.Now().Add(-2 * time.Hour)
	passed := wasAt.Add(time.Hour)
	noteKeyAllowance(p, []QuotaWindow{{
		Name: "7 days", Span: 7 * 24 * time.Hour, Used: 100,
		ResetsAt: &passed, Amount: 800, Limit: 800, Unit: "USD",
	}}, wasAt)
	keyAllowances.Lock()
	first := keyAllowances.m[id]
	if first == nil || !first.aAt.Equal(wasAt) {
		t.Fatalf("the first successful reading was not kept: %+v", first)
	}
	firstAt := first.aAt
	keyAllowances.Unlock()
	select {
	case got := <-started:
		t.Fatalf("the first reading told started: %s", got)
	default:
	}

	down.Store(true)
	read()
	down.Store(false)
	keyAllowances.Lock()
	failed := keyAllowances.m[id]
	if failed == nil || !failed.aAt.Equal(firstAt) {
		t.Fatalf("a failed reading moved the last successful reading: %+v", failed)
	}
	keyAllowances.Unlock()
	read()
	select {
	case got := <-started:
		if got != "/"+id {
			t.Fatalf("told %q, want %q", got, "/"+id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reset passed since the last successful reading: OnStarted was told nothing")
	}
	if n := asked.Load(); n != 2 {
		t.Fatalf("asked %d times, want the failed and elapsed-reset readings", n)
	}
}
