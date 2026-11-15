package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2008 (Gaudi-XIX): a free tier's daily allowance, refused as xkiro
// refuses it with no word of when it is back, rests the key until the time
// of day the user said it comes back, not 15 minutes, nor an hour when the
// vendor's Retry-After asked for longer than uni-switch trusts. A Retry-After
// shorter than that still stands over it.
func TestQuotaRestsUntilTheTimeOfDaySet(t *testing.T) {
	deadChain(t, "dead-a")
	srv := New()
	daily := []byte(`{"error":{"message":"You've reached today's free-model token quota","type":"rate_limit_error"}}`)
	at := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Minute)
	if err := provider.SetQuotaResetAt("mockdead", at.Format("15:04")+"Z"); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("mockdead")
	if err != nil {
		t.Fatal(err)
	}
	c := candidate{p: *p, model: "glm-5.3-flash", rest: "mockdead#x"}
	near := func(r Rest, want time.Time, by string) {
		t.Helper()
		if r.Why != failQuota || r.By != by || r.Until.Before(want.Add(-time.Second)) || r.Until.After(want.Add(time.Second)) {
			t.Errorf("rests %s by %q until %v, want by %q until %v", r.Why, r.By, r.Until, by, want)
		}
	}
	near(srv.restAfter(c, 429, http.Header{}, daily), at, "quota")
	// a Retry-After of two hours, cut to the hour uni-switch trusts: the
	// user's three hours stand
	near(srv.restAfter(c, 429, http.Header{"Retry-After": {"7200"}}, daily), at, "quota")
	// two minutes is the vendor's word, and stands
	now := time.Now()
	r := srv.restAfter(c, 429, http.Header{"Retry-After": {"120"}}, daily)
	near(r, now.Add(2*time.Minute), "retry-after")

	// a time already gone by today is tomorrow's
	past := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	if err := provider.SetQuotaResetAt("mockdead", past.Format("15:04")+"+00:00"); err != nil {
		t.Fatal(err)
	}
	p, _ = provider.Find("mockdead")
	c.p = *p
	near(srv.restAfter(c, 429, http.Header{}, daily), past.Add(24*time.Hour), "quota")

	// a number of minutes set after it is the one that counts
	if err := provider.SetQuotaRest("mockdead", 90); err != nil {
		t.Fatal(err)
	}
	p, _ = provider.Find("mockdead")
	c.p = *p
	if p.QuotaResetAt != "" {
		t.Fatalf("a rest of minutes kept the time of day %q", p.QuotaResetAt)
	}
	now = time.Now()
	near(srv.restAfter(c, 429, http.Header{}, daily), now.Add(90*time.Minute), "quota")
}
