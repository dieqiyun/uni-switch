package usage

import (
	"math"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A call to a DeepSeek model billed by the hour is costed at the hour it
// was made, not the hour the page is read: double its off-peak list price
// at peak (Thursday 02:00 UTC), the list price off-peak (11:00) and on a
// weekend. A call to another model at those same times costs what it did,
// and so does one at a price the user set (no Peak). Both the record's
// cost (Totals, Groups) and a packed gateway block's are.
func TestCostAtTheCallsHour(t *testing.T) {
	flash := catalog.Price{Input: 0.15, Output: 0.6, CacheRead: 0.003, Peak: catalog.PeakDeepSeek}
	other := catalog.Price{Input: 2, Output: 8, CacheRead: 0.2}
	peak := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)
	off := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	weekend := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-12 }
	rec := func(at time.Time) Record {
		return Record{Time: at, Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000}
	}
	base := 0.15 + 0.6 + 0.003
	for _, c := range []struct {
		at   time.Time
		want float64
	}{{peak, 2 * base}, {off, base}, {weekend, base}, {time.Time{}, base}} {
		if got := rec(c.at).CostAt(flash); !near(got, c.want) {
			t.Errorf("deepseek at %v: %v, want %v", c.at, got, c.want)
		}
		if got := rec(c.at).CostAt(other); !near(got, 10.2) {
			t.Errorf("other at %v: %v, want 10.2", c.at, got)
		}
		set := flash
		set.Peak = ""
		if got := rec(c.at).CostAt(set); !near(got, base) {
			t.Errorf("set price at %v: %v, want %v", c.at, got, base)
		}
	}

	// a gateway block's rows, priced in one pass
	var raw rowChunk
	times := []time.Time{peak, off, weekend}
	for i := 0; i < logBlockRows; i++ {
		at := times[i%3]
		raw.add(Row{Record: Record{Time: at, Provider: "deepseek", Model: "deepseek-flash", Input: 1_000_000, Output: 1_000_000}}, "", int64(i), false)
		raw.add(Row{Record: Record{Time: at, Provider: "openai", Model: "gpt-6-astra", Input: 1_000_000, Output: 1_000_000}}, "", int64(i), false)
	}
	b := newBlockPricer(provider.Renames{}, nil, func(r Record) *catalog.Price {
		if r.Provider == "deepseek" {
			return &flash
		}
		return &other
	})
	c, _ := b.priceBlock(raw.pack())
	if c == nil {
		t.Fatal("no block")
	}
	for i := range c.Rows {
		r := c.row(i)
		want := 10.0
		if r.Input == 1_000_000 && c.Strings[c.Shapes[c.Rows[i].Shape][1]] == "deepseek" {
			want = 0.75
			if r.Time.Equal(peak) {
				want = 1.5
			}
		}
		if !near(r.Cost, want) {
			t.Fatalf("row %d (%s at %v): %v, want %v", i, c.Strings[c.Shapes[c.Rows[i].Shape][1]], r.Time, r.Cost, want)
		}
	}
}
