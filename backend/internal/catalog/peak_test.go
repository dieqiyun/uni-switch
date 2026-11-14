package catalog

import (
	"testing"
	"time"
)

// DeepSeek's peak hours are 01:00 to 04:00 and 06:00 to 10:00 UTC, Monday
// to Friday: each edge is checked a minute either side, on a weekday, a
// weekend day and a Chinese public holiday, and at the time the call says,
// in whatever zone it is written in, never the machine's.
func TestPeakHours(t *testing.T) {
	at := func(day string, hm string) time.Time {
		v, err := time.Parse("2006-01-02 15:04", day+" "+hm)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	edges := map[string]bool{
		"00:59": false, "01:00": true, "03:59": true, "04:00": false,
		"05:59": false, "06:00": true, "09:59": true, "10:00": false,
		"00:00": false, "23:59": false, "12:00": false,
	}
	for _, c := range []struct {
		day      string
		deepseek bool // whether DeepSeek's peak hours apply that day
		weekdays bool // whether every-weekday ones do
	}{
		{"2026-10-29", true, true},   // a Thursday
		{"2026-10-26", true, true},   // a Monday
		{"2026-10-30", true, true},   // a Friday
		{"2026-10-31", false, false}, // a Saturday
		{"2026-11-01", false, false}, // a Sunday
		{"2026-10-05", false, true},  // a Monday in the National Day break
		{"2026-02-16", false, true},  // a Monday in the Spring Festival break
		{"2026-10-09", true, true},   // a Friday after the break
	} {
		for hm, peak := range edges {
			tm := at(c.day, hm)
			if got := PeakDeepSeek.At(tm); got != (peak && c.deepseek) {
				t.Errorf("deepseek %s %s: peak %v", c.day, hm, got)
			}
			if got := PeakWeekdays.At(tm); got != (peak && c.weekdays) {
				t.Errorf("weekdays %s %s: peak %v", c.day, hm, got)
			}
			// the same instant written in Shanghai's and New York's time
			for _, z := range []string{"Asia/Shanghai", "America/New_York"} {
				loc, err := time.LoadLocation(z)
				if err != nil {
					loc = time.FixedZone(z, map[string]int{"Asia/Shanghai": 8, "America/New_York": -4}[z]*3600)
				}
				if got := PeakDeepSeek.At(tm.In(loc)); got != (peak && c.deepseek) {
					t.Errorf("deepseek %s %s in %s: peak %v", c.day, hm, z, got)
				}
			}
			if Peak("").At(tm) {
				t.Errorf("no peak %s %s: peak", c.day, hm)
			}
		}
	}
	if PeakDeepSeek.At(time.Time{}) {
		t.Error("a call with no time is at peak")
	}
}

// Ollama Cloud's peak hours are 12:00 to 18:00 UTC, Monday to Friday,
// holidays too: each edge a minute either side, on a weekday, a weekend
// day and a Chinese holiday, in whatever zone the time is written in.
func TestOllamaPeakHours(t *testing.T) {
	far := []*time.Location{time.UTC, time.FixedZone("+14", 14*3600), time.FixedZone("-11", -11*3600), time.FixedZone("+0545", 5*3600+45*60)}
	edges := map[string]bool{
		"11:59": false, "12:00": true, "17:59": true, "18:00": false,
		"00:00": false, "02:00": false, "23:59": false, "15:30": true,
	}
	for _, c := range []struct {
		day     string
		weekday bool
	}{
		{"2026-10-28", true},  // a Wednesday
		{"2026-10-26", true},  // a Monday
		{"2026-10-30", true},  // a Friday
		{"2026-10-05", true},  // a Monday in China's National Day break
		{"2026-10-31", false}, // a Saturday
		{"2026-11-01", false}, // a Sunday
	} {
		for hm, peak := range edges {
			v, err := time.Parse("2006-01-02 15:04", c.day+" "+hm)
			if err != nil {
				t.Fatal(err)
			}
			for _, z := range far {
				if got := PeakOllama.At(v.In(z)); got != (peak && c.weekday) {
					t.Errorf("ollama %s %s UTC in %s: peak %v", c.day, hm, z, got)
				}
			}
		}
	}
	if PeakOllama.At(time.Time{}) {
		t.Error("a call with no time is at peak")
	}
	pro := Price{Input: 0.66, Output: 1.98, CacheRead: 0.022, Peak: PeakOllama}
	if got, want := pro.In(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)), (Price{Input: 1.32, Output: 3.96, CacheRead: 0.044}); !got.Same(want) {
		t.Errorf("peak: %+v, want %+v", got, want)
	}
	if got, want := pro.In(time.Date(2026, 10, 28, 11, 59, 0, 0, time.UTC)), (Price{Input: 0.66, Output: 1.98, CacheRead: 0.022}); !got.Same(want) {
		t.Errorf("off-peak: %+v, want %+v", got, want)
	}
}

// A price with no Peak holds at every hour; a peak-billed one doubles in
// its peak hours, every part of it, tiers too, and the price In gives is
// one that holds (In again doesn't double it twice).
func TestPriceIn(t *testing.T) {
	peak := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)
	off := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	flash := Price{Input: 0.15, Output: 0.6, CacheRead: 0.003, Peak: PeakDeepSeek}
	if got, want := flash.In(peak), (Price{Input: 0.3, Output: 1.2, CacheRead: 0.006}); !got.Same(want) {
		t.Errorf("peak: %+v, want %+v", got, want)
	}
	if got := flash.In(peak).In(peak); got.Input != 0.3 {
		t.Errorf("In twice: %+v", got)
	}
	if got, want := flash.In(off), (Price{Input: 0.15, Output: 0.6, CacheRead: 0.003}); !got.Same(want) {
		t.Errorf("off-peak: %+v, want %+v", got, want)
	}
	if got := flash.In(time.Time{}); got.Input != 0.15 {
		t.Errorf("no time: %+v", got)
	}
	// a model of another vendor at the same hours costs what it did
	other := Price{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	for _, tm := range []time.Time{peak, off} {
		if got := other.In(tm); !got.Same(other) {
			t.Errorf("%v: %+v, want %+v", tm, got, other)
		}
		if a, b := other.In(tm).CostSplit(1000, 1000, 1000, 1000, 0), other.CostSplit(1000, 1000, 1000, 1000, 0); a != b {
			t.Errorf("%v: cost %v, want %v", tm, a, b)
		}
	}
	if a, b := flash.In(peak).CostSplit(1e6, 1e6, 0, 0, 0), flash.In(off).CostSplit(1e6, 1e6, 0, 0, 0); a != 2*b {
		t.Errorf("peak cost %v, off-peak %v: want twice", a, b)
	}
}

// cnHolidays needs each year's days added once the State Council's notice
// is out (each November): this fails in a year it doesn't have, so the
// table is never silently a year behind.
func TestCNHolidaysCoverThisYear(t *testing.T) {
	if y := time.Now().In(chinaTime).Year(); len(cnHolidays[y]) == 0 {
		t.Fatalf("cnHolidays has no %d: add the State Council's notice for it", y)
	}
	for y, days := range cnHolidays {
		for _, d := range days {
			if _, err := time.Parse("2006-01-02", time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006")+"-"+d); err != nil {
				t.Errorf("%d %q: %v", y, d, err)
			}
		}
	}
}
