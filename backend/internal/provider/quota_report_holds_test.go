package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// #2333 (zhenghaowen0967): uni-switch quota --json and GET /v1/uni-switch/quotas
// tell each window's whole as the Usage page's card does, reckoned once
// over every card, and say why where it isn't told.
func TestQuotaReportTellsWindowHolds(t *testing.T) {
	was := ReckonHolds
	t.Cleanup(func() { ReckonHolds = was })
	calls, seen := 0, 0
	ReckonHolds = func(qs []SubscriptionQuota, now time.Time) []SubscriptionQuota {
		calls++
		seen = len(qs)
		out := make([]SubscriptionQuota, len(qs))
		for i, q := range qs {
			q.Windows = append([]QuotaWindow(nil), q.Windows...)
			for j := range q.Windows {
				switch q.Windows[j].Name {
				case "5 hours":
					q.Windows[j].Holds = &WindowHolds{Tokens: 1200000, Cost: 4.5, Priced: true, Used: 20}
				case "7 days":
					q.Windows[j].HoldsWhy = HoldsLow
				}
			}
			out[i] = q
		}
		return out
	}
	subs := []SubscriptionQuota{{Provider: "codex", User: "sam@example.com", Windows: []QuotaWindow{{Name: "5 hours", Used: 20}, {Name: "7 days", Used: 3}}}}
	plans := []SubscriptionQuota{{Provider: "glm", User: "Key", Windows: []QuotaWindow{{Name: "Monthly", Used: 9}}}}
	balances := []SubscriptionQuota{{Provider: "deepseek", Balance: "$5.00"}}
	r := quotaReport(subs, plans, balances, time.Now())
	if calls != 1 || seen != 3 {
		t.Fatalf("reckoned %d times over %d cards, want once over all 3", calls, seen)
	}
	if len(r) != 3 || r[0].Kind != "subscription" || r[1].Kind != "plan" || r[2].Kind != "balance" {
		t.Fatalf("cards out of their groups: %+v", r)
	}
	w := r[0].Windows
	if h := w[0].Holds; h == nil || h.Tokens != 1200000 || h.Cost != 4.5 || !h.Priced {
		t.Fatalf("5 hours told no whole: %+v", w[0])
	}
	if w[1].Holds != nil || w[1].HoldsWhy != HoldsLow {
		t.Fatalf("7 days: %+v, want no whole and why %q", w[1], HoldsLow)
	}
	b, _ := json.Marshal(r[0])
	for _, k := range []string{`"holds":{"tokens":1200000,"cost":4.5,"priced":true`, `"holdsWhy":"low"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("JSON has no %s: %s", k, b)
		}
	}
	if b, _ := json.Marshal(r[1]); strings.Contains(string(b), "holds") {
		t.Errorf("a window told nothing says holds: %s", b)
	}

	// where usage isn't linked in, no whole and no reason
	ReckonHolds = nil
	if r := quotaReport(subs, nil, nil, time.Now()); r[0].Windows[0].Holds != nil || r[0].Windows[1].HoldsWhy != "" {
		t.Fatalf("told without a reckoner: %+v", r[0].Windows)
	}
}
