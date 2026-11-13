package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2008 (Gaudi-XIX): a provider's own rest cap (`uni-switch provider rest-cap`)
// is where a key that keeps failing, or keeps being rate limited as soon
// as its rest is over, stops doubling: past the gateway's 10 and 30
// minutes for a free key that is down for hours, or short of them.
func TestOwnRestCap(t *testing.T) {
	s := &Server{}
	near := func(d, want time.Duration) bool { return d > want-5*time.Second && d <= want }
	ladder := func(name string, capMin, status int, body string, want ...time.Duration) {
		t.Helper()
		c := candidate{p: provider.Provider{ID: "capped-" + name, Key: "k", RestCap: capMin}, model: "m", rest: "capped-" + name + "#k"}
		id := c.restKey()
		for i, w := range want {
			r := s.restAfter(c, status, http.Header{}, []byte(body))
			if !near(time.Until(r.Until), w) {
				t.Fatalf("%s #%d: rests %v by %s (%s), want %v", name, i+1, time.Until(r.Until), r.By, r.Why, w)
			}
			restingUntil.Lock()
			restingUntil.m[id] = time.Now().Add(-time.Second)
			restingUntil.Unlock()
		}
	}
	rate := `{"error":{"message":"Too many requests","type":"rate_limit_error"}}`
	fail := `{"error":{"message":"upstream connect error","type":"api_error"}}`
	m := time.Minute
	ladder("rate-long", 240, 429, rate, m, 2*m, 4*m, 8*m, 16*m, 32*m, 64*m, 128*m, 240*m, 240*m)
	ladder("rate-short", 3, 429, rate, m, 2*m, 3*m, 3*m)
	ladder("rate-own", 0, 429, rate, m, 2*m, 4*m, 8*m, 16*m, 30*m, 30*m)
	ladder("fail-long", 120, 502, fail, m, 2*m, 4*m, 8*m, 16*m, 32*m, 64*m, 120*m)
	ladder("fail-own", 0, 502, fail, m, 2*m, 4*m, 8*m, 10*m, 10*m)
}

func TestRestCapOr(t *testing.T) {
	if d := (provider.Provider{RestCap: 90}).RestCapOr(time.Minute); d != 90*time.Minute {
		t.Error(d)
	}
	if d := (provider.Provider{}).RestCapOr(time.Minute); d != time.Minute {
		t.Error(d)
	}
	if d := (provider.Provider{RestCap: 99999999}).RestCapOr(time.Minute); d != provider.MaxQuotaRest*time.Minute {
		t.Error(d)
	}
}

// Kismet Alan on Discord: a relay or account pool whose unknown errors
// come now and then, the next request often going through, is set never
// to rest after one (`uni-switch provider rest-cap <id> off`). Its 5xx and its
// dropped connection rest it no more, nor count toward a backoff; what
// says why still rests it as before: a rate limit (uni-switch's own 30
// minutes cap), out of quota, its key refused, its model not served.
func TestRestCapOff(t *testing.T) {
	s := &Server{}
	c := candidate{p: provider.Provider{ID: "nofail", Key: "k", RestCap: provider.RestCapOff}, model: "m", rest: "nofail#k"}
	id := c.restKey()
	t.Cleanup(func() {
		clearRest(id)
		clearRest(c.restID())
		routed.Lock()
		delete(routed.failures, id)
		delete(routed.failures, c.restID())
		routed.Unlock()
	})
	fail := `{"error":{"message":"upstream connect error","type":"api_error"}}`
	for i, status := range []int{500, 502, 503, 504, 520} {
		r := s.restAfter(c, status, http.Header{}, []byte(fail))
		if r.Why != failOther || r.By != "off" || r.Until.After(time.Now()) {
			t.Fatalf("#%d %d: rests %v by %s (%s), want no rest", i+1, status, time.Until(r.Until), r.By, r.Why)
		}
		if resting(c) {
			t.Fatalf("#%d %d: set aside", i+1, status)
		}
	}
	routed.Lock()
	n := routed.failures[id]
	routed.Unlock()
	if n != 0 {
		t.Fatalf("%d failures counted", n)
	}
	// what says why rests it still
	for _, k := range []struct {
		status int
		body   string
		why    string
	}{
		{401, `{"error":{"message":"invalid api key","type":"api_error"}}`, failOther},
		{403, `{"error":{"message":"forbidden","type":"api_error"}}`, failOther},
		{429, `{"error":{"message":"Too many requests","type":"rate_limit_error"}}`, failRate},
		{429, `{"error":{"message":"You exceeded your current quota, please check your plan","type":"insufficient_quota"}}`, failCredit},
		{404, `{"error":{"message":"The model m does not exist or you do not have access to it","type":"invalid_request_error"}}`, failOther},
	} {
		clearRest(id)
		clearRest(c.restID())
		r := s.restAfter(c, k.status, http.Header{}, []byte(k.body))
		if r.Why != k.why || r.By == "off" || !r.Until.After(time.Now()) || !resting(c) {
			t.Fatalf("%d %s: rests %v by %s (%s), want a rest as %s", k.status, k.body, time.Until(r.Until), r.By, r.Why, k.why)
		}
	}
	// a rate limit's cap is uni-switch's own 30 minutes, not none
	if d := c.p.RestCapOr(longestRateRest); d != longestRateRest {
		t.Fatalf("rate cap %v", d)
	}
}
