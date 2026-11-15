package provider

import (
	"testing"
	"time"
)

// at is a local time on the week of Monday 2026-09-28.
func at(day, hour, min int) time.Time {
	return time.Date(2026, 9, 27+day, hour, min, 0, 0, time.Local) // day 0 is Sunday
}

// A rule's hours hold on the machine's clock, past midnight when they end
// before they begin, and on the day they begin on.
func TestRuleTimeWindow(t *testing.T) {
	day := TimeWindow{From: "09:00", To: "18:00", Days: []string{"mon", "tue", "wed", "thu", "fri"}}
	night := TimeWindow{From: "22:00", To: "08:00", Days: []string{"fri"}}
	whole := TimeWindow{From: "00:00", To: "00:00", Days: []string{"sat", "sun"}}
	for _, c := range []struct {
		w    TimeWindow
		at   time.Time
		want bool
	}{
		{day, at(1, 9, 0), true},    // Monday, as it opens
		{day, at(1, 17, 59), true},  // Monday, before it ends
		{day, at(1, 18, 0), false},  // as it ends
		{day, at(1, 8, 59), false},  // before
		{day, at(6, 12, 0), false},  // Saturday
		{night, at(5, 23, 0), true}, // Friday night
		{night, at(6, 3, 0), true},  // Saturday's early hours: Friday's window
		{night, at(6, 8, 0), false}, // as it ends
		{night, at(5, 3, 0), false}, // Friday's early hours: Thursday's window
		{night, at(6, 23, 0), false},
		{whole, at(0, 0, 0), true},
		{whole, at(6, 23, 59), true},
		{whole, at(1, 12, 0), false},
		{TimeWindow{From: "22:00", To: "08:00"}, at(3, 7, 0), true},
		{TimeWindow{From: "22:00", To: "08:00"}, at(3, 12, 0), false},
		{day, time.Time{}, false}, // no time is in no hours
	} {
		if got := c.w.Holds(c.at); got != c.want {
			t.Errorf("%s at %s: %v, want %v", c.w.Text(), c.at.Format("Mon 15:04"), got, c.want)
		}
	}
}
