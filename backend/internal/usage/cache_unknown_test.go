package usage

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// readBack is r as the ledger reads it back: from a chunk as written, priced
// into a block, and packed to its archive and out again.
func readBack(t *testing.T, r Record) []Row {
	t.Helper()
	made := func() *rowChunk {
		var chunk rowChunk
		for i := 0; i < 80; i++ {
			chunk.add(Row{Record: r}, "", int64(i), false)
		}
		return &chunk
	}
	b := newBlockPricer(provider.Renames{}, nil, func(Record) *catalog.Price { return nil })
	defer b.release()
	raw := made()
	return []Row{raw.row(0), b.block(raw).row(0), b.block(made().pack()).row(0)}
}

// A call whose vendor counted no cache (a plugin's uni_switch_cache_unknown:
// Kiro tells only credits) is read back from the ledger as one, so its
// CacheRead of 0 is never read as a miss (#3464). A vendor that counted a
// miss stays one, and a Kiro call kept before its plugin said so is read
// as uncounted: Kiro never counted its cache.
func TestCacheUnknownKeptInTheLedger(t *testing.T) {
	at := time.Now()
	for _, c := range []struct {
		name string
		r    Record
		want bool
	}{
		{"said so", Record{Time: at, Provider: "zcode", Model: "glm-5", Input: 148000, Output: 19, CacheUnknown: true, Status: 200}, true},
		{"a counted miss", Record{Time: at, Provider: "anthropic", Model: "claude-sonnet-4-5", Input: 148000, Output: 19, Status: 200}, false},
		{"Kiro, kept before", Record{Time: at, Provider: "kiro", Model: "claude-sonnet-4.5", Input: 148000, Output: 19, Status: 200}, true},
		{"Kiro's plugin id, kept before", Record{Time: at, Provider: "kiro-plugin", Model: "claude-sonnet-4.5", Input: 148000, Output: 19, Status: 200}, true},
		{"Kiro with cache counted", Record{Time: at, Provider: "kiro", Model: "claude-sonnet-4.5", Input: 1000, CacheRead: 9000, Output: 19, Status: 200}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			for i, row := range readBack(t, c.r) {
				if row.CacheUnknown != c.want {
					t.Fatalf("read %d: cache unknown %v, want %v", i, row.CacheUnknown, c.want)
				}
			}
		})
	}
}

// A hit rate leaves out the prompt of calls whose vendor counted no cache:
// a mixed ledger isn't diluted by them, and one of only those has none,
// where it read 0% (zeewell on X, #3464).
func TestHitRateLeavesUncountedOut(t *testing.T) {
	at := time.Now()
	counted := Record{Time: at, Provider: "anthropic", Model: "claude-sonnet-4-5", Input: 500, CacheRead: 9000, CacheWrite: 500, Output: 10, Status: 200}
	kiro := Record{Time: at, Provider: "kiro", Model: "claude-sonnet-4.5", Input: 148000, Output: 19, CacheUnknown: true, Status: 200}
	// kept before the plugin said so: Kiro never counts its cache
	kiroBefore := Record{Time: at, Provider: "kiro-plugin", Model: "claude-sonnet-4.5", Input: 90000, Output: 19, Status: 200}
	for _, c := range []struct {
		name  string
		rs    []Record
		rate  float64
		known bool
		calls int
	}{
		{"mixed", []Record{counted, kiro, kiro, kiroBefore}, 0.9, true, 3},
		{"only uncounted", []Record{kiro, kiroBefore}, 0, false, 2},
		{"only counted", []Record{counted}, 0.9, true, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			var byRecord, byRow Totals
			for _, r := range c.rs {
				byRecord.add(r, nil)
				byRow.addRow(Row{Record: r})
			}
			for _, tot := range []Totals{byRecord, byRow} {
				rate, known := tot.HitRate()
				if known != c.known || rate != c.rate || tot.CacheUnknownCalls != c.calls {
					t.Fatalf("hit rate %v (%v), %d left out; want %v (%v), %d: %+v", rate, known, tot.CacheUnknownCalls, c.rate, c.known, c.calls, tot)
				}
			}
		})
	}
}
