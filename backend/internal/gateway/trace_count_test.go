package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #659: an account whose vendor counts its allowance in amounts (a
// WorkBuddy account's credits) is told in the trace with that count, for
// the Routing page to say "355 / 500 credits" beside the share; one
// counted in shares alone is told as before.
func TestTraceTellsTheCount(t *testing.T) {
	p := provider.Provider{ID: "workbuddy", Name: "WorkBuddy", Account: &provider.Account{Agent: "workbuddy", User: "me", Plan: "Free"}}
	c := candidate{p: p, rest: "workbuddy:me", model: "deepseek-v4.1-flash"}
	now := time.Now()
	a := provider.Allowance{{Used: 71, Amount: 355, Of: 500, Unit: "credits"}}
	u, r := a.For(c.model, now)
	amt, of, unit := a.Count(c.model, now)
	wg := weighing{lefts: map[allowanceKey]left{c.allowanceKey(): {used: u, renews: r, amount: amt, of: of, unit: unit}}}
	w := weighed(c, p, wg, false, "")
	if !w.Known || w.Used != 71 || w.Amount != 355 || w.Limit != 500 || w.Unit != "credits" {
		t.Fatalf("weighed = %+v", w)
	}
	b, _ := json.Marshal(w)
	if !strings.Contains(string(b), `"amount":355,"limit":500,"unit":"credits"`) {
		t.Fatalf("json: %s", b)
	}
	wg.lefts[c.allowanceKey()] = left{used: 30}
	if b, _ := json.Marshal(weighed(c, p, wg, false, "")); strings.Contains(string(b), `"limit"`) || strings.Contains(string(b), `"unit"`) {
		t.Fatalf("a share alone: %s", b)
	}
}
