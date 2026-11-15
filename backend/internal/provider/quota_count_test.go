package provider

import (
	"testing"
	"time"
)

// #659: a window counted in amounts (WorkBuddy's credits) says its count
// as used or as left, as its share is said; one that isn't keeps the
// vendor's own text.
func TestQuotaWindowCount(t *testing.T) {
	w := QuotaWindow{Name: "Credits", Used: 71, Display: "3550 / 5000", Amount: 3550, Limit: 5000, Unit: "credits"}
	if got := w.Count(false); got != "3,550 / 5,000 credits" {
		t.Fatalf("used: %q", got)
	}
	if got := w.Count(true); got != "1,450 / 5,000 credits" {
		t.Fatalf("left: %q", got)
	}
	frac := QuotaWindow{Amount: 438.88000002, Limit: 3300, Unit: "credits"}
	if got := frac.Count(false); got != "438.88 / 3,300 credits" {
		t.Fatalf("fraction: %q", got)
	}
	if got := (QuotaWindow{Amount: 1300, Limit: 1200}).Count(true); got != "0 / 1,200" {
		t.Fatalf("overspent, no unit: %q", got)
	}
	if got := (QuotaWindow{Display: "$2.50"}).Count(true); got != "$2.50" {
		t.Fatalf("display: %q", got)
	}
	if got := groupedNumber(1234567.5); got != "1,234,567.5" {
		t.Fatalf("grouped: %q", got)
	}
}

// Routing tells the count of the window its share is of: the fullest that
// counts the model, emptied when its reset has passed.
func TestAllowanceCount(t *testing.T) {
	now := time.Now()
	a := allowanceOf([]QuotaWindow{
		{Name: "Week", Used: 20, Amount: 200, Limit: 1000, Unit: "credits"},
		{Name: "Opus", Used: 60, Model: "opus", Amount: 6, Limit: 10, Unit: "requests"},
	}, now)
	if amt, of, unit := a.Count("claude-opus", now); amt != 6 || of != 10 || unit != "requests" {
		t.Fatalf("opus: %v %v %q", amt, of, unit)
	}
	if amt, of, unit := a.Count("gpt", now); amt != 200 || of != 1000 || unit != "credits" {
		t.Fatalf("gpt: %v %v %q", amt, of, unit)
	}
	past := now.Add(-time.Hour)
	a = allowanceOf([]QuotaWindow{{Name: "Credits", Used: 50, ResetsAt: &past, Amount: 50, Limit: 100, Unit: "credits"}}, now)
	if amt, of, _ := a.Count("x", now); amt != 0 || of != 100 {
		t.Fatalf("renewed: %v of %v", amt, of)
	}
}
