package provider

import (
	"encoding/json"
	"testing"
	"time"
)

// #4791 (hayden4608 on Discord, handong66): four accounts of one plan,
// three with 100% of 5 hours left and 0% of 7 days, one with 0% of 5 hours
// and 22% of 7 days. The three can't spend their 5 hours until their week
// starts again, so their 5 hours are held by it; the fourth's week isn't
// held by its five hours, which start again first.
func TestHeldWindowsReportersCase(t *testing.T) {
	now := time.Unix(1_793_620_800, 0) // 2026-11-02 12:00 UTC
	// /wham/usage's rate_limit, as Codex reads it
	codex := func(user string, fiveUsed, weekUsed float64, fiveIn, weekIn int64) SubscriptionQuota {
		var u codexUsage
		b := []byte(`{"plan_type":"plus","rate_limit":{"allowed":true,` +
			`"primary_window":{"used_percent":` + jsonNum(fiveUsed) + `,"limit_window_seconds":18000,"reset_at":` + jsonNum(float64(now.Unix()+fiveIn)) + `},` +
			`"secondary_window":{"used_percent":` + jsonNum(weekUsed) + `,"limit_window_seconds":604800,"reset_at":` + jsonNum(float64(now.Unix()+weekIn)) + `}}}`)
		if err := json.Unmarshal(b, &u); err != nil {
			t.Fatal(err)
		}
		return SubscriptionQuota{Provider: "codex", User: user, Plan: "plus", Windows: []QuotaWindow{u.RateLimit.Primary.window(), u.RateLimit.Secondary.window()}}
	}
	qs := []SubscriptionQuota{
		codex("a@example.com", 0, 100, 5*3600, 3*86400),
		codex("b@example.com", 0, 100, 5*3600, 2*86400),
		codex("c@example.com", 0, 100, 5*3600, 86400),
		codex("d@example.com", 100, 78, 2*3600, 4*86400),
	}
	out := WithHeldWindows(qs, now)
	for i := range 3 {
		if got := out[i].Windows[0].HeldBy; got != "7 days" {
			t.Errorf("%s: 5 hours held by %q, want 7 days", out[i].User, got)
		}
		if got := out[i].Windows[1].HeldBy; got != "" {
			t.Errorf("%s: 7 days held by %q, want none", out[i].User, got)
		}
	}
	for _, w := range out[3].Windows {
		if w.HeldBy != "" {
			t.Errorf("d: %s held by %q: a week behind five hours used up keeps its 22%% for after them", w.Name, w.HeldBy)
		}
	}
	for _, q := range qs {
		for _, w := range q.Windows {
			if w.HeldBy != "" {
				t.Fatalf("the cached cards were written: %s %s", q.User, w.Name)
			}
		}
	}
}

func TestHeldWindowsOnlyByWhatStopsTheAccount(t *testing.T) {
	now := time.Unix(1_793_620_800, 0)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	five := QuotaWindow{Name: "5 hours", Used: 0, ResetsAt: at(time.Hour), Span: 5 * time.Hour}
	for _, c := range []struct {
		why  string
		week QuotaWindow
		user string
		held bool
	}{
		{"a week used up", QuotaWindow{Name: "7 days", Used: 100, ResetsAt: at(48 * time.Hour), Span: 168 * time.Hour}, "", true},
		{"a week used up, its reset not told", QuotaWindow{Name: "7 days", Used: 100}, "", true},
		{"one model's week (Claude's Opus)", QuotaWindow{Name: "7 days Opus", Used: 100, ResetsAt: at(48 * time.Hour), Model: "opus"}, "", false},
		{"money aside", QuotaWindow{Name: "Extra usage", Used: 100, Aside: true}, "", false},
		{"a week whose reset has passed", QuotaWindow{Name: "7 days", Used: 100, ResetsAt: at(-time.Minute)}, "", false},
		{"a week that starts again first", QuotaWindow{Name: "7 days", Used: 100, ResetsAt: at(30 * time.Minute)}, "", false},
		{"a week with room", QuotaWindow{Name: "7 days", Used: 99, ResetsAt: at(48 * time.Hour)}, "", false},
		{"a ChatGPT account going on on its credits", QuotaWindow{Name: "7 days", Used: 100, ResetsAt: at(48 * time.Hour), Credits: true}, "e@example.com", false},
	} {
		q := SubscriptionQuota{Provider: "codex", User: c.user, Windows: []QuotaWindow{five, c.week}}
		got := WithHeldWindows([]SubscriptionQuota{q}, now)[0].Windows[0].HeldBy
		if (got != "") != c.held {
			t.Errorf("%s: 5 hours held by %q, want held %v", c.why, got, c.held)
		}
	}
	// another uni-switch's card is as it sent it, a failed reading as it was
	q := SubscriptionQuota{Provider: "codex", From: "studio", Windows: []QuotaWindow{five, {Name: "7 days", Used: 100}}}
	if got := WithHeldWindows([]SubscriptionQuota{q}, now)[0].Windows[0].HeldBy; got != "" {
		t.Errorf("another uni-switch's card held by %q", got)
	}
}
