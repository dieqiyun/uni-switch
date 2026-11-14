package gui

import (
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The menu bar's Pace number (settings.TrayPace, fflake33 on Discord): off,
// the rows are as they were; on, each window's figure has its Pace beside
// it, turned where what is left is shown, and the tooltip says it too.
func TestTrayUsagePace(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	in := func(d time.Duration) *time.Time { at := now.Add(d); return &at }
	q := provider.SubscriptionQuota{Provider: "claude", Name: "Claude", User: "a@b.c", Windows: []provider.QuotaWindow{
		// 2h 50m of 5h gone by (56.7%), 42.2% used: 14 points under
		{Name: "5-hour", Used: 42.2, ResetsAt: in(2*time.Hour + 10*time.Minute), Span: 5 * time.Hour},
		// 4 of 7 days gone by (57.1%), 70% used: 13 points over
		{Name: "Weekly", Used: 70, ResetsAt: in(3 * 24 * time.Hour), Span: 7 * 24 * time.Hour},
	}}

	label, tip := trayUsageText(q, now, false, false)
	if label != "42% · 70%" || tip != "Claude\n5-hour 42.2% used · resets in 2h 10m\nWeekly 70% used · resets in 3d 0h" {
		t.Errorf("pace off: %q %q", label, tip)
	}

	label, tip = trayUsageText(q, now, false, true)
	if label != "42% −14% · 70% +13%" {
		t.Errorf("used label %q", label)
	}
	if want := "Claude\n5-hour 42.2% used · pace −14% · resets in 2h 10m\nWeekly 70% used · pace +13% · resets in 3d 0h"; tip != want {
		t.Errorf("used tip %q, want %q", tip, want)
	}

	label, tip = trayUsageText(q, now, true, true)
	if label != "58% +14% · 30% −13%" {
		t.Errorf("left label %q", label)
	}
	if want := "Claude\n5-hour 57.8% left · pace +14% · resets in 2h 10m\nWeekly 30% left · pace −13% · resets in 3d 0h"; tip != want {
		t.Errorf("left tip %q, want %q", tip, want)
	}

	// each row still fits the menu bar whole, at its widest
	cells, _, _ := trayUsageView([]provider.SubscriptionQuota{q}, now, false, true)
	if len(cells) != 1 || !slices.Equal(cells[0].Rows, []string{"42% −14%", "70% +13%"}) {
		t.Fatalf("cells %+v", cells)
	}
	if r := trayRow("100% −100%"); r != "100% −100%" {
		t.Errorf("widest row cut: %q", r)
	}

	// a window whose start can't be known, or with no limit, has no Pace
	q.Windows = []provider.QuotaWindow{
		{Name: "Credits", Used: 40, ResetsAt: in(time.Hour)},
		{Name: "Daily", Used: 0, Unlimited: true, ResetsAt: in(time.Hour)},
	}
	if label, _ := trayUsageText(q, now, false, true); label != "40% · 0%" {
		t.Errorf("unknown span %q", label)
	}

	for p, want := range map[float64]string{12.4: "+12%", -8: "−8%", 0.4: "±0%", -0.4: "±0%"} {
		if got := trayPace(p, false); got != want {
			t.Errorf("trayPace(%v) = %q, want %q", p, got, want)
		}
	}
}
