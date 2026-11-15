package provider

import (
	"math"
	"testing"
	"time"
)

func TestWindowPace(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }

	// half of a 10-hour window gone by: 62% used is 12 points over, 42% 8 under
	w := QuotaWindow{Name: "10h", Used: 62, ResetsAt: at(5 * time.Hour), Span: 10 * time.Hour}
	if p, ok := w.Pace(now, now); !ok || math.Abs(p-12) > 1e-9 {
		t.Errorf("over: %v %v", p, ok)
	}
	w.Used = 42
	if p, ok := w.Pace(now, now); !ok || math.Abs(p+8) > 1e-9 {
		t.Errorf("under: %v %v", p, ok)
	}
	// the span from the window's name when the vendor gave none
	if p, ok := (QuotaWindow{Name: "Weekly", Used: 50, ResetsAt: at(84 * time.Hour)}).Pace(now, now); !ok || math.Abs(p) > 1e-9 {
		t.Errorf("weekly: %v %v", p, ok)
	}
	// a reset given as seconds counts from when the card was read
	readAt := now.Add(-time.Hour)
	if p, ok := (QuotaWindow{Used: 50, ResetSecs: 5400, Span: 2 * time.Hour}).Pace(readAt, now); !ok || math.Abs(p+25) > 1e-9 {
		t.Errorf("reset secs: %v %v", p, ok)
	}
	// used past the whole counts as the whole
	if p, ok := (QuotaWindow{Name: "10h", Used: 130, ResetsAt: at(5 * time.Hour), Span: 10 * time.Hour}).Pace(now, now); !ok || math.Abs(p-50) > 1e-9 {
		t.Errorf("clamped: %v %v", p, ok)
	}
	for name, w := range map[string]QuotaWindow{
		"unlimited":    {Name: "10h", Unlimited: true, ResetsAt: at(time.Hour), Span: 10 * time.Hour},
		"no reset":     {Name: "Weekly", Used: 10},
		"no span":      {Name: "Credits", Used: 10, ResetsAt: at(time.Hour)},
		"already over": {Name: "10h", Used: 10, ResetsAt: at(-time.Minute), Span: 10 * time.Hour},
		"not begun":    {Name: "1h", Used: 10, ResetsAt: at(2 * time.Hour), Span: time.Hour},
	} {
		if p, ok := w.Pace(now, now); ok {
			t.Errorf("%s: pace %v", name, p)
		}
	}
}

func TestWithWindowStarts(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	monthEnd := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	readAt := now.Add(-time.Hour)
	qs := []SubscriptionQuota{{Name: "A", ReadAt: &readAt, Windows: []QuotaWindow{
		{Name: "5-hour", ResetsAt: &reset, Span: 5 * time.Hour},
		{Name: "Monthly", ResetsAt: &monthEnd},
		{Name: "Seconds", ResetSecs: 3600, Span: 2 * time.Hour},
		{Name: "Credits", ResetsAt: &reset},
		{Name: "Daily", Unlimited: true, ResetsAt: &reset},
	}}, {Name: "B"}}
	got := WithWindowStarts(qs, now)

	ws := got[0].Windows
	if ws[0].StartsAt == nil || !ws[0].StartsAt.Equal(reset.Add(-5*time.Hour)) {
		t.Errorf("5-hour start %v", ws[0].StartsAt)
	}
	if ws[1].StartsAt == nil || !ws[1].StartsAt.Equal(time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("monthly start %v", ws[1].StartsAt)
	}
	// ResetSecs counts from when the card was read, not from now
	if ws[2].StartsAt == nil || !ws[2].StartsAt.Equal(readAt.Add(-time.Hour)) {
		t.Errorf("seconds start %v", ws[2].StartsAt)
	}
	if ws[3].StartsAt != nil || ws[4].StartsAt != nil {
		t.Errorf("unknown span or no limit given a start: %v %v", ws[3].StartsAt, ws[4].StartsAt)
	}
	if len(got) != 2 || got[1].Name != "B" {
		t.Errorf("cards %+v", got)
	}
	// the caches' own cards are left as they were
	for _, w := range qs[0].Windows {
		if w.StartsAt != nil {
			t.Fatalf("input window %q given a start", w.Name)
		}
	}
}
