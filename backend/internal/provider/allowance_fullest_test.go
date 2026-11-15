package provider

import (
	"testing"
	"time"
)

// Fullest is when the window For's share is of renews (#1778): Factory's
// week, 32% used, not its month, 16% and the biggest. Of two as full, the
// later; one whose reset has passed is empty again; a window of other
// models doesn't count.
func TestFullestRenewsWithTheShare(t *testing.T) {
	now := time.Now()
	five, week, month := now.Add(4*time.Hour), now.Add(3*24*time.Hour), now.Add(27*24*time.Hour)
	a := Allowance{
		{Used: 0, Resets: five, Span: 5 * time.Hour},
		{Used: 32, Resets: week, Span: 7 * 24 * time.Hour},
		{Used: 16, Resets: month, Span: 30 * 24 * time.Hour},
		{Used: 90, Resets: five, Span: 7 * 24 * time.Hour, Model: "glm"},
	}
	if used, renews := a.For("claude-opus-5-5", now); used != 32 || !renews[0].Equal(month) {
		t.Fatalf("For: %v %v", used, renews)
	}
	if got := a.Fullest("claude-opus-5-5", now); !got.Equal(week) {
		t.Fatalf("Fullest %v, want the week's %v", got, week)
	}
	if got := a.Fullest("glm-5.2", now); !got.Equal(five) {
		t.Fatalf("a model with a window of its own: %v, want %v", got, five)
	}
	tie := Allowance{{Used: 100, Resets: five, Span: 5 * time.Hour}, {Used: 100, Resets: week, Span: 7 * 24 * time.Hour}}
	if got := tie.Fullest("m", now); !got.Equal(week) {
		t.Fatalf("two used up: %v, want the later %v", got, week)
	}
	// the week renewed: it is empty, the month the fullest
	if got := a.Fullest("claude-opus-5-5", week.Add(time.Minute)); !got.Equal(month) {
		t.Fatalf("after the week renewed: %v, want the month's %v", got, month)
	}
	if got := (Allowance{{Used: 40, Span: 7 * 24 * time.Hour}}).Fullest("m", now); !got.IsZero() {
		t.Fatalf("a reset not known: %v", got)
	}
}
