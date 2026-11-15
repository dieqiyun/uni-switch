package gateway

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func forgetLifts(t *testing.T) {
	t.Helper()
	clear := func() {
		restingUntil.Lock()
		restingUntil.lifted = map[string]time.Time{}
		restingUntil.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

// zoecats on Discord: an account rate limited (429) with Sink on, lifted
// with Try now in the provider's editor, is asked again in its place, not
// left at the back of the order until uni-switch restarts.
func TestTryNowPutsASunkAccountBack(t *testing.T) {
	forgetSinks(t)
	forgetLifts(t)
	old := allowances
	defer func() { allowances = old }()
	allowances = func(string) map[string]provider.Allowance {
		out := map[string]provider.Allowance{}
		for _, u := range []string{"a@x.com", "b@x.com"} {
			out[u] = provider.Allowance{{Used: 10, Span: 30 * 24 * time.Hour, Resets: time.Now().Add(24 * time.Hour)}}
		}
		return out
	}
	s := New()
	acct := func(user string) candidate {
		return candidate{p: provider.Provider{ID: "workbuddy", Routing: provider.Ordered, Account: &provider.Account{Agent: "workbuddy", User: user}}, model: "m", rest: "workbuddy@" + user}
	}
	cs := []candidate{acct("a@x.com"), acct("b@x.com")}
	p := provider.Provider{ID: "workbuddy", Routing: provider.Ordered, Sink: true}
	order := func() []string {
		out, _ := weigh(p, slices.Clone(cs), "m", "")
		out, _ = restLast(out, planned{order: make([]Weighed, len(out))})
		return whoOf(out)
	}
	rate := []byte(`{"error":{"message":"Too many requests","type":"rate_limit_error"}}`)
	if r := s.restAfter(cs[0], 429, http.Header{}, rate); r.Why != failRate {
		t.Fatalf("a: %s", r.Why)
	}
	if got := order(); !slices.Equal(got, []string{"b@x.com", "a@x.com"}) {
		t.Fatalf("rate limited: order %v", got)
	}
	if !s.Unrest(cs[0].restKey()) {
		t.Fatal("Unrest: a wasn't resting")
	}
	if got := order(); !slices.Equal(got, []string{"a@x.com", "b@x.com"}) {
		t.Fatalf("after Try now: order %v, want a back in its place", got)
	}

	// the same through UnrestAll (a key's value replaced)
	s.restAfter(cs[0], 429, http.Header{}, rate)
	s.UnrestAll(cs[0].restKey())
	if got := order(); !slices.Equal(got, []string{"a@x.com", "b@x.com"}) {
		t.Fatalf("after UnrestAll: order %v", got)
	}
}

// A rest lifted before its time is told to the Routing page, wherever it
// was lifted: the trace says when, by the rest's key, and a page waiting
// on the trace hears of it at once though no route changed. Before, the
// page kept showing the routes' old 429 until uni-switch restarted.
func TestTryNowToldToTheRoutingPage(t *testing.T) {
	forgetSinks(t)
	forgetLifts(t)
	s := New()
	key := "fakeco#8a197f6f60"
	restingUntil.Lock()
	restingUntil.m[key] = time.Now().Add(10 * time.Minute)
	restingUntil.m[key+"/m1"] = time.Now().Add(10 * time.Minute)
	restingUntil.Unlock()

	st := s.Trace(context.Background(), 0, 0)
	if len(st.Lifted) != 0 {
		t.Fatalf("lifted before any lift: %v", st.Lifted)
	}
	seq := st.Seq

	woke := make(chan TraceState, 1)
	go func() { woke <- s.Trace(context.Background(), seq, time.Minute) }()
	// the waiter is waiting once it made the wake channel
	for {
		s.trace.mu.Lock()
		w := s.trace.wake != nil
		s.trace.mu.Unlock()
		if w {
			break
		}
		time.Sleep(time.Millisecond)
	}
	before := time.Now()
	s.Unrest(key)
	select {
	case st = <-woke:
	case <-time.After(30 * time.Second):
		t.Fatal("a page waiting on the trace didn't hear of the lift")
	}
	if at, ok := st.Lifted[key]; !ok || at.Before(before) {
		t.Fatalf("trace lifted %v, want %s at or after %s", st.Lifted, key, before)
	}
	if st.Seq <= seq {
		t.Fatalf("seq %d, not past %d", st.Seq, seq)
	}
	// read again from where it is, it doesn't wake on the same lift
	if again := s.Trace(context.Background(), st.Seq, 0); again.Seq != st.Seq {
		t.Fatalf("seq moved without a change: %d to %d", st.Seq, again.Seq)
	}

	// UnrestAll tells each model's rest too
	s.UnrestAll(key)
	st = s.Trace(context.Background(), 0, 0)
	if _, ok := st.Lifted[key+"/m1"]; !ok {
		t.Fatalf("UnrestAll: lifted %v, want %s/m1", st.Lifted, key)
	}
}

// A quota rest lifted when the account's windows renewed is told too.
func TestRenewedToldToTheRoutingPage(t *testing.T) {
	forgetSinks(t)
	forgetLifts(t)
	key := "codex@a@x.com"
	restingUntil.Lock()
	restingUntil.m[key] = time.Now().Add(time.Hour)
	restingUntil.note[key] = Rest{Why: failQuota, agent: "codex", user: "a@x.com", Key: key}
	restingUntil.Unlock()
	renewed("codex", "A@x.com")
	if _, ok := liftedRests()[key]; !ok {
		t.Fatalf("renewed: lifted %v, want %s", liftedRests(), key)
	}
}
