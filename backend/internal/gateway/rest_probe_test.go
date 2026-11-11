package gateway

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// probing starts s's probe loop with short steps and a stand-in for the
// vendor's list, and ends both with the test.
func probing(t *testing.T, list func(context.Context, provider.Provider) error) *Server {
	t.Helper()
	steps, was := restProbeSteps, listForProbe
	restProbeSteps = []time.Duration{20 * time.Millisecond, 40 * time.Millisecond}
	listForProbe = list
	s := &Server{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.keepProbingRests(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		restProbeSteps, listForProbe = steps, was
	})
	untilState(t, "the loop to start", func() bool {
		restProbes.Lock()
		defer restProbes.Unlock()
		return restProbes.asking[s]
	})
	return s
}

// untilState waits for ok to hold, as long as a loaded machine may need.
func untilState(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 30s for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func probeKey(t *testing.T, id string) candidate {
	t.Helper()
	c := candidate{p: provider.Provider{ID: id, Key: "sk-" + id}, model: "m", rest: id}
	forget := func() {
		routed.Lock()
		delete(routed.failures, c.restKey())
		delete(routed.failures, c.restID())
		routed.Unlock()
		clearRest(c.restKey())
		clearRest(c.restID())
		probeAnswered(c.restKey())
		restProbes.Lock()
		delete(restProbes.m, c.restKey())
		restProbes.Unlock()
	}
	forget()
	t.Cleanup(forget)
	return c
}

func probeQueued(key string) bool {
	restProbes.Lock()
	defer restProbes.Unlock()
	_, ok := restProbes.m[key]
	return ok
}

const outage = `{"error":{"message":"upstream unavailable","type":"api_error"}}`

// heatonz on Discord: a key resting after a 5xx stayed out for its whole
// rest though its vendor was back. It is asked again in the background
// with its own key, and back as soon as it answers.
func TestRestProbeLiftsAKeyBackUp(t *testing.T) {
	var asked atomic.Int32
	var mu sync.Mutex
	var keys []string
	s := probing(t, func(_ context.Context, p provider.Provider) error {
		mu.Lock()
		keys = append(keys, p.Key)
		mu.Unlock()
		if asked.Add(1) == 1 {
			return errors.New("still down")
		}
		return nil
	})
	c := probeKey(t, "probe-up")
	r := s.restAfter(c, http.StatusServiceUnavailable, http.Header{}, []byte(outage))
	if !r.Probed || r.By != "backoff" || r.Key != c.restKey() {
		t.Fatalf("a 503 rests %+v, not asked again", r)
	}
	if _, ok := restOf(c.restKey()); !ok {
		t.Fatal("not resting")
	}
	untilState(t, "the rest to be lifted", func() bool { _, ok := restOf(c.restKey()); return !ok })
	if n := asked.Load(); n != 2 {
		t.Fatalf("asked %d times, want a failed ask and an answered one", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if keys[0] != "sk-probe-up" || keys[1] != "sk-probe-up" {
		t.Fatalf("asked with %v, not the resting key", keys)
	}
	if probeQueued(c.restKey()) {
		t.Fatal("still queued after it answered")
	}

	// the list answered and the requests still don't: no ask again until
	// a request is answered
	r = s.restAfter(c, http.StatusBadGateway, http.Header{}, []byte(outage))
	if r.Probed || probeQueued(c.restKey()) {
		t.Fatalf("failed again right after a probe lifted it, and asked again: %+v", r)
	}
	clearRest(c.restKey())
	served(c.restKey(), c.restKey(), 1)
	routed.Lock()
	delete(routed.failures, c.restID())
	routed.Unlock()
	if r = s.restAfter(c, http.StatusBadGateway, http.Header{}, []byte(outage)); !r.Probed {
		t.Fatalf("a later outage, after it answered, isn't asked again: %+v", r)
	}
}

// A rest whose end the vendor named, or that is about the key or the
// request, or one model, or a subscription's account, ends on time: none
// is asked again in the background.
func TestRestProbeLeavesOtherRests(t *testing.T) {
	var asked atomic.Int32
	s := probing(t, func(context.Context, provider.Provider) error { asked.Add(1); return nil })
	retryAfter := http.Header{}
	retryAfter.Set("Retry-After", "300")
	for _, tc := range []struct {
		name   string
		status int
		header http.Header
		body   string
		acct   bool
	}{
		{"retry-after", http.StatusServiceUnavailable, retryAfter, outage, false},
		{"rate limit", http.StatusTooManyRequests, http.Header{}, `{"error":{"message":"rate limit exceeded"}}`, false},
		{"quota", http.StatusTooManyRequests, http.Header{}, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota"}}`, false},
		{"refused key", http.StatusUnauthorized, http.Header{}, `{"error":{"message":"invalid api key"}}`, false},
		{"bad request", http.StatusBadRequest, http.Header{}, `{"error":{"message":"bad things"}}`, false},
		{"account", http.StatusServiceUnavailable, http.Header{}, outage, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := probeKey(t, "probe-"+tc.name)
			if tc.acct {
				c.p.Account = &provider.Account{Agent: "codex", User: "probe@example.com"}
				t.Cleanup(func() {
					routed.Lock()
					delete(routed.failures, c.restKey())
					delete(routed.failures, c.restID())
					routed.Unlock()
					clearRest(c.restKey())
					clearRest(c.restID())
				})
			}
			r := s.restAfter(c, tc.status, tc.header, []byte(tc.body))
			if r.Probed || probeQueued(r.Key) {
				t.Fatalf("%s (rests by %s, %s) asked again in the background", tc.name, r.By, r.Why)
			}
		})
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("asked %d times", n)
	}
}

// A gateway whose loop isn't running (a test's) asks nothing, and a loop
// that ends lets go of its keys once the asks under way have ended.
func TestRestProbeOnlyByARunningLoop(t *testing.T) {
	c := probeKey(t, "probe-idle")
	if r := (&Server{}).restAfter(c, http.StatusServiceUnavailable, http.Header{}, []byte(outage)); probeQueued(c.restKey()) {
		t.Fatalf("queued with no loop: %+v", r)
	}
	clearRest(c.restKey())

	steps, was := restProbeSteps, listForProbe
	t.Cleanup(func() { restProbeSteps, listForProbe = steps, was })
	restProbeSteps = []time.Duration{20 * time.Millisecond}
	started, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	listForProbe = func(ctx context.Context, _ provider.Provider) error {
		close(started)
		<-ctx.Done()
		<-release // an ask slow to see it was cut short
		close(ended)
		return ctx.Err()
	}
	s := &Server{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.keepProbingRests(ctx); close(done) }()
	untilState(t, "the loop to start", func() bool {
		restProbes.Lock()
		defer restProbes.Unlock()
		return restProbes.asking[s]
	})
	routed.Lock()
	delete(routed.failures, c.restKey())
	delete(routed.failures, c.restID())
	routed.Unlock()
	if r := s.restAfter(c, http.StatusServiceUnavailable, http.Header{}, []byte(outage)); !r.Probed {
		t.Fatalf("not asked again: %+v", r)
	}
	<-started
	cancel()
	// the loop waits for its ask: ended while it is held, it left it
	// behind. Only a loop that ends early can fail here, however slow the
	// machine; one ending early on a very slow one may pass unseen
	select {
	case <-done:
		t.Fatal("the loop ended before its ask did")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-done
	select {
	case <-ended:
	default:
		t.Fatal("the loop ended before its ask did")
	}
	if probeQueued(c.restKey()) {
		t.Fatal("an ended loop's key is still queued")
	}
	if _, ok := restOf(c.restKey()); !ok {
		t.Fatal("an ask cut short lifted the rest")
	}
}
