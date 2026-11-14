package usage

import (
	"testing"
	"time"
)

// Days the reader picked (#1492, nianlee-official) are a period of their own:
// the Usage page, the Requests page, the ledger and Direct count the calls of
// those days and nothing after the last, under a chart of those days, an hour
// each for one day and a week each past 60 days.
func TestPickedDaysArePeriod(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local))
	day := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, time.Local) }
	for _, at := range []time.Time{
		day(9, 30, 23), // the day before
		day(10, 1, 0),  // the first day's first hour
		day(10, 2, 9),
		day(10, 3, 23), // the last day's last hour
		day(10, 4, 0),  // the day after
		day(10, 9, 12),
	} {
		Append(Record{Time: at, Agent: "claude", Provider: "openai", Model: "m", Input: 10, Status: 200})
	}
	p, ok := RangeOf("2026-10-03", "2026-10-01") // either order
	if !ok || p != "2026-10-01..2026-10-03" || !p.Known() {
		t.Fatalf("RangeOf: %q %v", p, ok)
	}
	if !p.Since(now).Equal(day(10, 1, 0)) || !p.Until(now).Equal(day(10, 4, 0)) {
		t.Errorf("%s: from %v until %v", p, p.Since(now), p.Until(now))
	}
	s := Summarize(p)
	if s.Calls != 3 || s.Period != p || s.Bucket != "day" || len(s.Series) != 3 || !s.Series[0].Time.Equal(day(10, 1, 0)) {
		t.Errorf("Summarize(%s): %d calls, period %q, %d %s points; want 3 calls in 3 days", p, s.Calls, s.Period, len(s.Series), s.Bucket)
	}
	if pg := QueryPage(p, Filter{}, 0, 10); pg.Total != 3 {
		t.Errorf("QueryPage(%s): %d rows, want 3", p, pg.Total)
	}
	if l := LedgerOf(p, Filter{}); len(l.Rows) != 3 {
		t.Errorf("LedgerOf(%s): %d rows, want 3", p, len(l.Rows))
	}
	if d := Direct(p); d.Period != p || !d.Since.Equal(day(10, 1, 0)) {
		t.Errorf("Direct(%s): period %q since %v", p, d.Period, d.Since)
	}

	// one day: its hours
	one, _ := RangeOf("2026-10-09", "2026-10-09")
	if s := Summarize(one); s.Calls != 1 || s.Bucket != "hour" || len(s.Series) != 24 {
		t.Errorf("Summarize(%s): %d calls, %d %s points; want 1 in 24 hours", one, s.Calls, len(s.Series), s.Bucket)
	}
	// past 60 days: weeks from a Monday, and still none after the last day
	long, _ := RangeOf("2026-07-01", "2026-10-01")
	if s := Summarize(long); s.Calls != 2 || s.Bucket != "week" || s.Series[0].Time.Weekday() != time.Monday {
		t.Errorf("Summarize(%s): %d calls in %s points from %v; want 2 in weeks from a Monday", long, s.Calls, s.Bucket, s.Since)
	}

	for _, bad := range []Period{"2026-10-03..2026-10-01", "2026-13-01..2026-13-02", "x..y", "90d"} {
		if bad.Known() {
			t.Errorf("%q is known", bad)
		}
	}
}
