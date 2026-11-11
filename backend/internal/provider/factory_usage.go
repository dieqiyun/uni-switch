package provider

// PLUGIN-SERVED (see AGENTS.md): Factory ("factory") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-factory-auth (provider.BuiltinRetired). What
// is left here reads /api/billing/limits for tests of the gateway's pool
// routing (FactoryAllowanceForTest).

// How much of a Factory plan an account has used, as droid's /status asks
// it: GET /api/billing/limits. Standard usage runs in rolling 5-hour, weekly
// and monthly windows, each a percent used and when it ends; Droid Core, the
// open models Factory hosts, has windows of its own; extra usage is a
// balance in cents.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type factoryWindow struct {
	Used float64         `json:"usedPercent"`
	End  json.RawMessage `json:"windowEnd"`
}

type factoryPool struct {
	FiveHour *factoryWindow `json:"fiveHour"`
	Weekly   *factoryWindow `json:"weekly"`
	Monthly  *factoryWindow `json:"monthly"`
}

type factoryLimits struct {
	Limits struct {
		Standard *factoryPool `json:"standard"`
		Core     *factoryPool `json:"core"`
	} `json:"limits"`
	Extra        *float64 `json:"extraUsageBalanceCents"`
	ExtraAllowed bool     `json:"extraUsageAllowed"`
}

// factoryWhen reads a window's end: an ISO time or epoch milliseconds, as
// droid hands either to new Date.
func factoryWhen(raw json.RawMessage) *time.Time {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return &t
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			t := time.UnixMilli(n).UTC()
			return &t
		}
		return nil
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		t := time.UnixMilli(int64(n)).UTC()
		return &t
	}
	return nil
}

// factoryQuota turns the limits into windows: standard's by their span,
// then Droid Core's, then the extra usage left. For routing, standard counts
// the vendors' models and Core the open ones Factory hosts; with extra usage
// to spend, a window used up stops neither.
func factoryQuota(l factoryLimits) []QuotaWindow {
	var out []QuotaWindow
	extra := l.ExtraAllowed && l.Extra != nil && *l.Extra > 0
	add := func(p *factoryPool, prefix string, core bool) {
		if p == nil {
			return
		}
		for _, w := range []struct {
			win  *factoryWindow
			name string
			span time.Duration
		}{
			{p.FiveHour, "5 hours", 5 * time.Hour},
			{p.Weekly, "7 days", 7 * 24 * time.Hour},
			{p.Monthly, "30 days", 30 * 24 * time.Hour},
		} {
			if w.win == nil {
				continue
			}
			out = append(out, QuotaWindow{Name: prefix + w.name, Used: min(max(w.win.Used, 0), 100),
				Span: w.span, ResetsAt: factoryWhen(w.win.End), Aside: extra,
				matches: func(model string) bool { return factoryCore(model) == core }})
		}
	}
	add(l.Limits.Standard, "", false)
	add(l.Limits.Core, "Droid Core · ", true)
	if l.Extra != nil && *l.Extra > 0 {
		// a balance in cents, its display alone: no share, as Claude's
		// "Extra usage" row is (claudeOveragesWindow), and no history
		// point of a fictitious 100% left (#4612)
		out = append(out, QuotaWindow{Name: "Extra usage", Display: fmt.Sprintf("$%.2f", *l.Extra/100), Aside: true, Text: true})
	}
	return out
}

// FactoryAllowanceForTest is the allowance an account's /api/billing/limits
// answer gives routing, for the gateway's tests.
func FactoryAllowanceForTest(limits []byte, now time.Time) Allowance {
	var l factoryLimits
	if json.Unmarshal(limits, &l) != nil {
		return nil
	}
	return allowanceOf(factoryQuota(l), now)
}
