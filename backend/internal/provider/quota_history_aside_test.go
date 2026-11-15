package provider

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// Aside is routing: using the window up doesn't stop the account, which is
// the Cursor plugin's word for its Weekly window when on-demand or free
// credit can pay, and for Grok Bot always (chowyuan1314, #4612). The
// readings are real, so they keep their history line as any measured
// window's, as a pool's window already does; one whose display alone is
// told has no share and keeps none.
func TestQuotaHistoryKeepsAsideReadings(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t0 := time.Date(2026, 10, 31, 16, 51, 12, 0, time.UTC)
	reset := t0.Add(4 * 24 * time.Hour)
	card := func(used float64) []SubscriptionQuota {
		return []SubscriptionQuota{{Provider: "cursor", Name: "Cursor", User: "c@x.com", Windows: []QuotaWindow{
			{Name: "Weekly", Used: used, ResetsAt: &reset, Span: 7 * 24 * time.Hour, Aside: true},
			{Name: "Grok Bot", Used: used, ResetsAt: &reset, Span: 7 * 24 * time.Hour, Aside: true},
			{Name: "On-demand", Used: 0, Display: "$0.00", ResetsAt: &reset, Text: true, Aside: true},
		}}}
	}
	noteQuotaHistory(card(56.1), t0)
	noteQuotaHistory(card(70), t0.Add(time.Hour))
	hs := QuotaHistories(t0.Add(-time.Hour), "", "")
	if len(hs) != 1 || len(hs[0].Lines) != 2 {
		t.Fatalf("histories = %+v, want the Weekly and Grok Bot lines alone", hs)
	}
	for _, name := range []string{"Weekly", "Grok Bot"} {
		pts := lineOf(t, hs, "c@x.com", name)
		if len(pts) != 2 || pts[0].Left != 43.90 || pts[1].Left != 30 {
			t.Errorf("%s = %+v, want the readings' points", name, pts)
		}
	}
}

// Factory's extra usage is a balance in cents: its row is its display
// alone, as Claude's "Extra usage" row is, so it is Text and keeps no
// history point of a fictitious 100% left (#4612).
func TestFactoryExtraUsageIsTextAlone(t *testing.T) {
	l := factoryLimits{}
	l.ExtraAllowed = true
	l.Extra = fptr(500)
	var extra *QuotaWindow
	for i, w := range factoryQuota(l) {
		if w.Name == "Extra usage" {
			extra = &factoryQuota(l)[i]
		}
	}
	if extra == nil {
		t.Fatal("no Extra usage window")
	}
	if !extra.Text || !extra.Aside {
		t.Errorf("Extra usage: Text %v, Aside %v, want its text alone", extra.Text, extra.Aside)
	}
	if _, ok := pointOf(*extra, time.Now(), time.Now()); ok {
		t.Error("Extra usage produced a history point")
	}
}
