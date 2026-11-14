package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// #2451 (tianxia3111): a Command Code plan's key, on the keyed preset,
// showed its 5-hour and weekly windows as meters and the month's credits
// only as dollars left ($5.27). billing/credits leaves the plan out, so
// the key's card asks billing/subscriptions for it, as the signed-in
// subscription's card does, and the month is a meter beside the windows.
// The reply is the reporter's own.
func TestCommandCodeKeyCardHasCredits(t *testing.T) {
	var subs atomic.Int32
	var subsDown atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Host") + r.URL.Path {
		case "api.commandcode.ai/alpha/billing/credits":
			w.Write([]byte(`{"credits":{"belowThreshold":false,"creditThreshold":0,"monthlyCredits":5.2207933742,"purchasedCredits":0,"freeCredits":0},
 "windowLimits":{"limited":true,"exceeded":null,
   "fiveHour":{"used":0.0866816244,"cap":14,"exceeded":false,"resetAt":4102444800000},
   "weekly":{"used":0.8971969678,"cap":35,"exceeded":false,"resetAt":4102444800000}},
 "sandboxAccess":true}`))
		case "api.commandcode.ai/alpha/billing/subscriptions":
			subs.Add(1)
			if subsDown.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`{"success":true,"data":{"planId":"individual-go-monthly","status":"active"}}`))
		default:
			t.Errorf("asked %s%s", r.Header.Get("X-Host"), r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })

	p := Provider{ID: "commandcode", Key: "cmd-2451", Chat: "https://api.commandcode.ai/provider/v1"}
	src, _ := planQuotaSourceOf(p)
	plan, ws, _, err := planKeyWindows(context.Background(), p, src, p.Key)
	if err != nil || plan != "Go" || len(ws) != 3 {
		t.Fatalf("%v %q %+v", err, plan, ws)
	}
	if ws[0].Name != "5 hours" || ws[1].Name != "Weekly" {
		t.Errorf("windows: %+v", ws)
	}
	// Go gives 10 credits a month, usage-value units, not dollars; 5.22
	// of them are left
	if c := ws[2]; c.Name != "Credits" || c.Display != "4.78 / 10.00 credits" || c.Used < 47.7 || c.Used > 47.8 {
		t.Errorf("credits: %+v", c)
	}

	// the plan is asked again only after ten minutes, and the one read
	// before stands while it can't be read
	subsDown.Store(true)
	if _, ws, _, _ := planKeyWindows(context.Background(), p, src, p.Key); len(ws) != 3 || subs.Load() != 1 {
		t.Errorf("read again: %d asks, %+v", subs.Load(), ws)
	}
	cmdKeyPlans.Lock()
	seen := cmdKeyPlans.m[p.Key]
	seen.at = seen.at.Add(-11 * 60e9)
	cmdKeyPlans.m[p.Key] = seen
	cmdKeyPlans.Unlock()
	if plan, ws, _, _ := planKeyWindows(context.Background(), p, src, p.Key); len(ws) != 3 || plan != "Go" || subs.Load() != 2 {
		t.Errorf("plan unreadable: %d asks, %q %+v", subs.Load(), plan, ws)
	}

	// a key whose plan was never read has its windows alone
	q := p
	q.Key = "cmd-2451-new"
	if plan, ws, _, err := planKeyWindows(context.Background(), q, src, q.Key); err != nil || plan != "" || len(ws) != 2 {
		t.Errorf("plan never read: %v %q %+v", err, plan, ws)
	}
}

// The Credits window #2451 added renews when the plan's period does: the
// end is in the same billing/subscriptions reply the plan id is read
// from, and the card's other two windows each say when they start again.
func TestCommandCodeKeyCreditsRenewAtPeriodEnd(t *testing.T) {
	const end = int64(4102444800000) // 2100-01-01, in ms as cmdTime reads it
	periodEnd := end
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Host") + r.URL.Path {
		case "api.commandcode.ai/alpha/billing/credits":
			w.Write([]byte(`{"credits":{"monthlyCredits":5.22,"purchasedCredits":0,"freeCredits":0},
 "windowLimits":{"limited":true,
   "fiveHour":{"used":1,"cap":14,"resetAt":4102444800000},
   "weekly":{"used":2,"cap":35,"resetAt":4102444800000}}}`))
		case "api.commandcode.ai/alpha/billing/subscriptions":
			w.Write([]byte(`{"success":true,"data":{"planId":"individual-go-monthly","status":"active","currentPeriodEnd":` + strconv.FormatInt(periodEnd, 10) + `}}`))
		default:
			t.Errorf("asked %s%s", r.Header.Get("X-Host"), r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })

	p := Provider{ID: "commandcode", Key: "cmd-reset", Chat: "https://api.commandcode.ai/provider/v1"}
	src, _ := planQuotaSourceOf(p)
	_, ws, _, err := planKeyWindows(context.Background(), p, src, p.Key)
	if err != nil || len(ws) != 3 {
		t.Fatalf("%v %+v", err, ws)
	}
	if c := ws[2]; c.Name != "Credits" || c.ResetsAt == nil || c.ResetsAt.UnixMilli() != end {
		t.Errorf("the key card's credits have no reset: %+v", c)
	}

	// a period end already past says nothing about when they come back
	periodEnd = time.Now().Add(-time.Hour).UnixMilli()
	p.Key = "cmd-reset-past"
	_, ws, _, err = planKeyWindows(context.Background(), p, src, p.Key)
	if err != nil || len(ws) != 3 {
		t.Fatalf("%v %+v", err, ws)
	}
	if c := ws[2]; c.Name != "Credits" || c.ResetsAt != nil {
		t.Errorf("the key card's credits reset in the past: %+v", c)
	}
}
