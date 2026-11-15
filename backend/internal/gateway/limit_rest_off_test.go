package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// zoecats on Discord: a local aggregator says "quota exhausted" for a
// moment while it switches upstreams, and the half hour uni-switch rested its
// one key for it held every request back, answered "out of its allowance"
// without asking. With rests on quota and 429 turned off for the provider
// (LimitRestOff), the refusal goes to the agent as it came, nothing is set
// aside, and the next request is sent to the aggregator, which answers.
func TestLimitRestOffKeepsAsking(t *testing.T) {
	noRests()
	t.Cleanup(noRests)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var calls, failing atomic.Int32
	failing.Store(1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if failing.Load() == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":{"message":"quota exhausted","type":"insufficient_quota"}}`)
			return
		}
		io.WriteString(w, `{"id":"x","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "agg", Name: "Agg", Key: "k", Models: []string{"m1"}, Chat: up.URL + "/v1", LimitRestOff: true}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	req := `{"model":"agg/m1","messages":[{"role":"user","content":"hi"}]}`
	code, body := post(t, "/v1/chat/completions", req)
	if code != http.StatusTooManyRequests || !strings.Contains(body, "quota exhausted") {
		t.Fatalf("first: %d %s", code, body)
	}
	restingUntil.Lock()
	n := len(restingUntil.m)
	restingUntil.Unlock()
	if n != 0 {
		t.Fatalf("%d rests set after the refusal, want none", n)
	}
	before := calls.Load()
	// refused again: asked again, never answered from a rest
	code, body = post(t, "/v1/chat/completions", req)
	if code != http.StatusTooManyRequests || strings.Contains(body, "not asked again") || calls.Load() == before {
		t.Fatalf("second, still refusing: %d %s, upstream asked %d times", code, body, calls.Load()-before)
	}
	failing.Store(0)
	code, body = post(t, "/v1/chat/completions", req)
	if code != http.StatusOK || !strings.Contains(body, `"hi"`) {
		t.Fatalf("third, upstream back: %d %s", code, body)
	}
}

// The switch covers every refusal it names, and only those: out of quota
// or credit and rate limited, at any status, and any 429, rest nothing,
// count toward no backoff and sink nothing; a refused key still rests.
// Off by default: the same refusals rest a provider without it.
func TestLimitRestOffRestsNothingForALimit(t *testing.T) {
	s := &Server{}
	limits := []struct {
		status int
		body   string
	}{
		{429, `{"error":{"message":"quota exhausted","type":"insufficient_quota"}}`},
		{429, `{"error":{"message":"Too many requests","type":"rate_limit_error"}}`},
		{429, `{"error":{"message":"busy"}}`},
		{403, `{"error":{"message":"Your quota has been exhausted","type":"insufficient_quota"}}`},
		{402, `{"error":{"message":"Insufficient balance","type":"insufficient_balance"}}`},
	}
	for _, off := range []bool{true, false} {
		c := candidate{p: provider.Provider{ID: "lim", Key: "k", LimitRestOff: off}, model: "m", rest: "lim#k"}
		id := c.restKey()
		clean := func() {
			clearRest(id)
			clearRest(c.restID())
			routed.Lock()
			delete(routed.failures, id)
			delete(routed.failures, c.restID())
			routed.Unlock()
			sunk.Lock()
			delete(sunk.m, id)
			sunk.Unlock()
		}
		clean()
		t.Cleanup(clean)
		for _, k := range limits {
			clean()
			r := s.restAfter(c, k.status, http.Header{}, []byte(k.body))
			if off && (r.By != "off" || r.Until.After(time.Now()) || resting(c)) {
				t.Fatalf("off, %d %s: rests %v by %s (%s), want none", k.status, k.body, time.Until(r.Until), r.By, r.Why)
			}
			if !off && (r.By == "off" || !resting(c)) {
				t.Fatalf("on, %d %s: not rested (%s by %s)", k.status, k.body, r.Why, r.By)
			}
			routed.Lock()
			f := routed.failures[id]
			routed.Unlock()
			sunk.Lock()
			_, sank := sunk.m[id]
			sunk.Unlock()
			if off && (f != 0 || sank) {
				t.Fatalf("off, %d: %d failures counted, sunk %v", k.status, f, sank)
			}
		}
		if off {
			clean()
			r := s.restAfter(c, 401, http.Header{}, []byte(`{"error":{"message":"invalid api key"}}`))
			if r.By == "off" || !resting(c) {
				t.Fatalf("a refused key isn't rested: %s by %s", r.Why, r.By)
			}
		}
	}
}
