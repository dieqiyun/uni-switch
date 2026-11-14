package usage

import (
	"testing"
	"time"
)

// A rolling window (#2075) is a period of its own: it ends now and began
// that long ago, where the presets begin at a local midnight — what
// `uni-switch usage --last 24h` asks for when today is 35 minutes old and a
// preset would give a whole calendar day.
func TestRollingWindowIsAPeriod(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local))
	at := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, time.Local) }
	for _, when := range []time.Time{
		at(10, 9, 11), // 25 hours ago
		at(10, 9, 13), // 23 hours ago
		at(10, 10, 11),
	} {
		Append(Record{Time: when, Agent: "claude", Provider: "openai", Model: "m", Input: 10, Status: 200})
	}
	p, ok := LastOf(24 * time.Hour)
	if !ok || !p.Known() || p.Last() != 24*time.Hour { // the period's text is its duration's own form ("last:24h0m0s"), which reads back as 24h
		t.Fatalf("LastOf(24h): %q %v", p, ok)
	}
	if got := p.Last(); got != 24*time.Hour {
		t.Errorf("Last(): %v, want 24h", got)
	}
	if !p.Since(now).Equal(at(10, 9, 12)) {
		t.Errorf("Since: %v, want Oct 9 12:00", p.Since(now))
	}
	if !p.Until(now).IsZero() {
		t.Errorf("Until: %v, want zero (it runs up to now)", p.Until(now))
	}
	s := Summarize(p)
	if s.Calls != 2 || s.Period != p {
		t.Errorf("Summarize(%s): %d calls, period %q; want 2", p, s.Calls, s.Period)
	}
	if s.Bucket != "hour" || len(s.Series) != 25 {
		t.Errorf("Summarize(%s): %d %s points; want 25 hours", p, len(s.Series), s.Bucket)
	}
	if !s.Series[0].Time.Equal(at(10, 9, 12)) || !s.Series[24].Time.Equal(at(10, 10, 12)) {
		t.Errorf("Summarize(%s): points %v..%v", p, s.Series[0].Time, s.Series[24].Time)
	}
	if l := LedgerOf(p, Filter{}); len(l.Rows) != 2 {
		t.Errorf("LedgerOf(%s): %d rows, want 2", p, len(l.Rows))
	}
	if d := Direct(p); d.Period != p || !d.Since.Equal(at(10, 9, 12)) {
		t.Errorf("Direct(%s): period %q since %v", p, d.Period, d.Since)
	}
}

// Longer than two days, a rolling window's chart is a day each.
func TestRollingWindowOverTwoDaysIsDays(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local))
	Append(Record{Time: now.Add(-time.Hour), Agent: "claude", Provider: "openai", Model: "m", Input: 10, Status: 200})
	p, _ := LastOf(72 * time.Hour)
	if s := Summarize(p); s.Calls != 1 || s.Bucket != "day" || len(s.Series) != 4 {
		t.Errorf("Summarize(%s): %d calls, %d %s points; want 1 in 4 days", p, s.Calls, len(s.Series), s.Bucket)
	}
}

// Minutes work too, and so does a fractional day the CLI's --days builds.
func TestRollingWindowFractions(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local))
	Append(Record{Time: now.Add(-30 * time.Minute), Agent: "claude", Provider: "openai", Model: "m", Input: 10, Status: 200})
	p, _ := LastOf(90 * time.Minute)
	if s := Summarize(p); s.Calls != 1 || s.Bucket != "hour" || len(s.Series) != 2 {
		t.Errorf("Summarize(%s): %d calls, %d %s points; want 1 in 2 hours", p, s.Calls, len(s.Series), s.Bucket)
	}
	if q, _ := LastOf(36 * time.Hour); q.Last() != 36*time.Hour {
		t.Errorf("LastOf(36h): %v", q.Last())
	}
}

func TestRollingWindowRejectsJunk(t *testing.T) {
	for _, bad := range []Period{"last:0s", "last:abc", "last:-5m", "last:", "last", "90d"} {
		if bad.Known() {
			t.Errorf("%q is known", bad)
		}
		if bad.Last() != 0 {
			t.Errorf("%q lasts %v", bad, bad.Last())
		}
	}
	if p, ok := LastOf(0); ok || p != "" {
		t.Errorf("LastOf(0): %q %v", p, ok)
	}
	if p, ok := LastOf(-time.Hour); ok || p != "" {
		t.Errorf("LastOf(-1h): %q %v", p, ok)
	}
}

// A rolling window's start moves with the clock while the log stays as it
// is: a summary asked again later doesn't count a call that has fallen out
// of it, as a kept summary would (#2075).
func TestRollingWindowIsNotKeptStale(t *testing.T) {
	pageHome(t)
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	holdClock(t, at)
	Append(Record{Time: at.Add(-30 * time.Minute), Agent: "claude", Provider: "openai", Model: "m", Input: 10, Status: 200})
	p, _ := LastOf(time.Hour)
	if s := SummarizeTop(p, 10); s.Calls != 1 {
		t.Fatalf("at noon: %d calls, want 1", s.Calls)
	}
	holdClock(t, at.Add(time.Hour))
	if s := SummarizeTop(p, 10); s.Calls != 0 {
		t.Errorf("an hour later: %d calls, want 0 (the call is 90 minutes old)", s.Calls)
	}
}
