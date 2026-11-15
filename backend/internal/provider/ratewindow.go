package provider

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RateWindow is a time-of-day window a provider bills at its own rate
// (#2463): a relay that charges 2× or 3× at peak and a discount off
// peak. Start and End are "HH:MM" on this computer's clock, End may be
// "24:00"; Start after End crosses midnight, and the window then belongs
// to the day it starts on, its Days among them. Days are time.Weekday's
// numbers (Sunday 0), empty for every day. Rate is what the provider
// bills against the official price inside the window, on top of
// PriceRate: from just over 0 to 1000, at most three decimals. The first
// window that holds a call's time wins; outside every window the price
// rate alone counts. The window's rate scales a price the user set for a
// model too, which the price rate alone leaves as set.
type RateWindow struct {
	Days  []int   `json:"days,omitempty"`
	Start string  `json:"start"`
	End   string  `json:"end"`
	Rate  float64 `json:"rate"`
}

// RateWindowsMax is how many windows one provider can have (#2463).
const RateWindowsMax = 8

// ParseClock reads "HH:MM" as minutes into the day: 0 for 00:00 through
// 1440 for 24:00, which only an end may be. ok is false for anything
// else.
func ParseClock(s string) (min int, ok bool) {
	h, m, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found || len(h) == 0 || len(h) > 2 || !isDigits(h) || len(m) != 2 || !isDigits(m) {
		return 0, false
	}
	hh, err := strconv.Atoi(h)
	if err != nil || hh > 24 {
		return 0, false
	}
	mm, err := strconv.Atoi(m)
	if err != nil || mm > 59 {
		return 0, false
	}
	if min = hh*60 + mm; min > 1440 {
		return 0, false
	}
	return min, true
}

// isDigits says whether s is ASCII digits alone.
func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// CheckRateWindows says what is wrong with a set of a provider's rate
// windows, "" when nothing. A window's rate stands to the same rule as a
// price rate but is never 0: 0 would read as the price as listed, which
// is what being outside every window already means.
func CheckRateWindows(ws []RateWindow) string {
	if len(ws) > RateWindowsMax {
		return fmt.Sprintf("at most %d rate windows", RateWindowsMax)
	}
	for i, w := range ws {
		start, ok := ParseClock(w.Start)
		if !ok {
			return fmt.Sprintf("rate window %d: the start is a time like 09:00, not %s", i+1, w.Start)
		}
		end, ok := ParseClock(w.End)
		if !ok {
			return fmt.Sprintf("rate window %d: the end is a time like 18:00, not %s", i+1, w.End)
		}
		if start == 1440 {
			return fmt.Sprintf("rate window %d: the start is a time like 09:00, not %s", i+1, w.Start)
		}
		if start == end {
			return fmt.Sprintf("rate window %d: the start and the end are both %s", i+1, w.Start)
		}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return fmt.Sprintf("rate window %d: a day is 0 (Sunday) to 6 (Saturday), not %d", i+1, d)
			}
		}
		if r := w.Rate; !(r > 0) || math.IsNaN(r) || r > 1000 {
			return fmt.Sprintf("rate window %d: the rate is a number from 0 to 1000, not %v", i+1, r)
		}
		if math.Abs(w.Rate*1000-math.Round(w.Rate*1000)) > 1e-6 {
			return fmt.Sprintf("rate window %d: the rate has at most three decimals, not %v", i+1, w.Rate)
		}
	}
	return ""
}

// RateAt is the rate the windows bill at at t, on this computer's clock:
// 0 when no window holds t, which means the price rate alone, and for a
// zero t, a price asked for with no call's time (a memo of the price
// shared by many calls) being the base. The first window that holds t
// wins. A window's span is from its start in and to
// its end out; one that crosses midnight belongs to the day it starts
// on, so its early hours read as that day's.
func RateAt(ws []RateWindow, t time.Time) float64 {
	if len(ws) == 0 || t.IsZero() {
		return 0
	}
	// t's own zone is whatever wrote it: a call reported to
	// /v1/uni-switch/usage as "…Z" parses in UTC, and its hour read there
	// missed a window by the zone's offset
	t = t.Local()
	m := t.Hour()*60 + t.Minute()
	day := int(t.Weekday())
	for _, w := range ws {
		start, ok1 := ParseClock(w.Start)
		end, ok2 := ParseClock(w.End)
		if !ok1 || !ok2 || start == end {
			continue
		}
		var in bool
		d := day
		if start < end {
			in = m >= start && m < end
		} else {
			in = m >= start || m < end
			if m < end {
				// past midnight: the window began yesterday
				d = (d + 6) % 7
			}
		}
		if !in || len(w.Days) > 0 && !slices.Contains(w.Days, d) {
			continue
		}
		return w.Rate
	}
	return 0
}

// RateWindowsOf is the rate windows of the provider with id, or of the one
// renamed from it, none for a provider not known: for a caller holding
// a base price to read each call's time against (#2463).
func RateWindowsOf(id string) []RateWindow {
	p, ok := byIDOrWas(id)
	if !ok {
		return nil
	}
	return p.RateWindows
}

// RateAt is the rate the provider's windows bill at at t, 0 for none.
func (p Provider) RateAt(t time.Time) float64 {
	return RateAt(p.RateWindows, t)
}
