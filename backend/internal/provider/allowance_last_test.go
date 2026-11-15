package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// While the first reading of the accounts' allowances since uni-switch started
// is still out, routing goes by what each said last, kept on disk — not by
// none, which counts every account unused and put the first in order
// first, until the reading came back and put the other first again
// (vincentzhang on Discord: a restart moved a session to the first
// account, the next back to the second, and its prompt cache was lost).
func TestAllowancesBeforeFirstReading(t *testing.T) {
	home := signIn(t)
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	reset := func() {
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale = nil
		usedCache.Unlock()
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
	}
	reset()
	t.Cleanup(reset)

	used := map[string]float64{"acct-1": 12, "acct-work@example.com": 97}
	hold := make(chan struct{})
	var slow atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			select {
			case <-hold:
			case <-r.Context().Done():
			}
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "pro", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": used[r.Header.Get("chatgpt-account-id")], "limit_window_seconds": 18000}}})
	}))
	t.Cleanup(fake.Close)
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	oldWait := firstWait
	firstWait = 50 * time.Millisecond
	t.Cleanup(func() { firstWait = oldWait })
	var reading chan struct{} // the read Allowances leaves out
	t.Cleanup(func() {
		close(hold)
		// it lands while CodexBase, which it reads, is still the fake's,
		// and before the reset: not in the next test that asks for codex's
		// allowances, which would take it for its own
		if reading != nil {
			<-reading
		}
	})

	// read once, as before the restart: kept on disk
	if u := LoginUsage(context.Background(), "codex"); len(u) != 2 {
		t.Fatalf("usage %+v", u)
	}
	// a restart, and the vendor slow to say this time
	reset()
	slow.Store(true)
	a := Allowances("codex")
	usedCache.Lock()
	reading = usedCache.loading["codex"]
	usedCache.Unlock()
	now := time.Now()
	for user, want := range map[string]float64{"me@example.com": 12, "work@example.com": 97} {
		if got, _ := a[user].For("gpt-5.5", now); got != want {
			t.Fatalf("%s before the first reading: used %v, want the last %v (%+v)", user, got, want, a)
		}
	}
}

// Under Unwaited, the first ask answers from what was last known without
// waiting firstWait for the vendor: each agent's model list asks whether a
// model sees images, and each first ask held uni-switch's start for firstWait.
func TestUnwaitedAllowancesDontWait(t *testing.T) {
	signIn(t)
	rememberLogins(true)
	reset := func() {
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale = nil
		usedCache.Unlock()
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
	}
	reset()
	t.Cleanup(reset)

	hold := make(chan struct{})
	var slow atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			select {
			case <-hold:
			case <-r.Context().Done():
			}
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "pro", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": 12, "limit_window_seconds": 18000}}})
	}))
	t.Cleanup(fake.Close)
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	oldWait := firstWait
	firstWait = time.Minute
	t.Cleanup(func() { firstWait = oldWait })
	var reading chan struct{}
	t.Cleanup(func() {
		close(hold)
		if reading != nil {
			<-reading
		}
	})

	if u := LoginUsage(context.Background(), "codex"); len(u) != 1 {
		t.Fatalf("usage %+v", u)
	}
	reset()
	slow.Store(true)
	release := Unwaited()
	began := time.Now()
	a := Allowances("codex")
	took := time.Since(began)
	release()
	release() // a second release changes nothing
	usedCache.Lock()
	reading = usedCache.loading["codex"]
	usedCache.Unlock()
	if took > 10*time.Second {
		t.Fatalf("Allowances under Unwaited took %v", took)
	}
	if got, _ := a["me@example.com"].For("gpt-5.5", time.Now()); got != 12 {
		t.Fatalf("under Unwaited: used %v, want the last 12 (%+v)", got, a)
	}
	if n := unwaited.Load(); n != 0 {
		t.Fatalf("unwaited left at %d after release", n)
	}
}
