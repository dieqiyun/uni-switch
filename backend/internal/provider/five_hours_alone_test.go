package provider

import (
	"testing"
	"time"
)

// An account with the five hours alone (Claude Enterprise) has nothing
// longer to keep for: Pace goes by what its five hours lose at their
// reset, the whole span when not started, and Renewal has a five hours
// not started renewing within five hours — unless the reading may leave
// a week out (a Claude account only heard of as Claude Code answered),
// which goes as a fresh week with the share used, as before (#576).
func TestFiveHoursAlone(t *testing.T) {
	now := time.Now()
	five := 5 * time.Hour
	near := func(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

	a := Allowance{{Used: 20, Resets: now.Add(2 * time.Hour), Span: five}}
	if p, due := a.Pace("m", now); !near(p, 40) || !due.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("started: %v %v", p, due)
	}
	idle := Allowance{{Used: 0, Span: five}}
	if p, _ := idle.Pace("m", now); !near(p, 20) {
		t.Fatalf("not started: %v", p)
	}
	ws := []QuotaWindow{{Name: "5 hours", Used: 20, ResetsAt: ptr(now.Add(2 * time.Hour)), Span: five, partial: true}}
	if p, _ := allowanceOf(ws, now).Pace("m", now); !near(p, 80/(7*24.0)) {
		t.Fatalf("partial reading, a fresh week: %v", p)
	}
	// beside a week, the week decides as ever
	both := Allowance{{Used: 0, Span: five}, {Used: 40, Resets: now.Add(60 * time.Hour), Span: 7 * 24 * time.Hour}}
	if p, _ := both.Pace("m", now); !near(p, 1) {
		t.Fatalf("with a week: %v", p)
	}

	if r := idle.Renewal("m", now); len(r) != 1 || !r[0].Equal(now.Add(five)) {
		t.Fatalf("renewal not started: %v", r)
	}
	if _, r := idle.For("m", now); len(r) != 1 || !r[0].IsZero() {
		t.Fatalf("For still says not known: %v", r)
	}
	if r := (Allowance{{Used: 3}}).Renewal("m", now); len(r) != 1 || !r[0].IsZero() {
		t.Fatalf("no span, not known: %v", r)
	}
}

// claudeWindows marks partial what only Claude Code's answers told.
func TestClaudeHeardOnlyIsPartial(t *testing.T) {
	e := claudeUsageEntry{ws: []QuotaWindow{{Name: "5 hours", Used: 10, Span: 5 * time.Hour}}}
	if ws := e.windows(time.Now()); !ws[0].partial {
		t.Fatal("heard only: partial")
	}
	e.whole = true
	if ws := e.windows(time.Now()); ws[0].partial {
		t.Fatal("read by /usage: whole")
	}
}
