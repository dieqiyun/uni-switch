package cli

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	stats "github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// uni-switch usage --last and --days read a rolling window ending now (#2075):
// what --csv rows and the summary both cover, where the presets begin at a
// local midnight.
func TestUsageRollingWindow(t *testing.T) {
	groupsHome(t)
	now := holdUsageClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	stats.Append(stats.Record{Time: now.Add(-time.Hour), Agent: "codex", Provider: "a", Model: "m", Input: 10, Status: 200})
	stats.Append(stats.Record{Time: now.Add(-25 * time.Hour), Agent: "claude", Provider: "b", Model: "m", Input: 10, Status: 200})
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "--csv", "--last", "24h"}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 { // the header and the call an hour old, not the one 25 hours old
		t.Fatalf("%d rows, want 2:\n%s", len(recs), b.String())
	}
	b.Reset()
	if err := usageTo(&b, []string{"usage", "--csv", "--days", "1.5"}); err != nil {
		t.Fatal(err)
	}
	recs, _ = csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if len(recs) != 3 { // 36 hours: both calls
		t.Fatalf("--days 1.5: %d rows, want 3", len(recs))
	}
	b.Reset()
	if err := usageTo(&b, []string{"usage", "--last", "24h"}); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); !strings.Contains(s, "last 1 day") || !strings.Contains(s, "1 call") {
		t.Errorf("summary says neither its window nor its count:\n%s", s)
	}
}

func TestUsageRollingWindowRejectsJunk(t *testing.T) {
	groupsHome(t)
	for _, args := range [][]string{
		{"usage", "--last", "abc"},
		{"usage", "--last", "0s"},
		{"usage", "--days", "0"},
		{"usage", "--days", "-1"},
		{"usage", "--days"},
		{"usage", "--last"},
		{"usage", "--last", "24h", "--days", "1"},
		{"usage", "--last", "24h", "7d"}, // which window was meant can't be told
		{"usage", "--days", "NaN"},
		{"usage", "--days", "Inf"},
		{"usage", "--days", "1e30"},
	} {
		if err := usageTo(&strings.Builder{}, args); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

// --last composes with --account as a preset does: the window's calls that
// account answered, and no other's (#2075, #557).
func TestUsageRollingWindowWithAccount(t *testing.T) {
	groupsHome(t)
	now := holdUsageClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	stats.Append(stats.Record{Time: now.Add(-time.Hour), Agent: "codex", Provider: "a", ProviderAccount: "me@x", Model: "m", Input: 10, Status: 200})
	stats.Append(stats.Record{Time: now.Add(-2 * time.Hour), Agent: "codex", Provider: "a", ProviderAccount: "other@x", Model: "m", Input: 10, Status: 200})
	stats.Append(stats.Record{Time: now.Add(-30 * time.Hour), Agent: "codex", Provider: "a", ProviderAccount: "me@x", Model: "m", Input: 10, Status: 200})
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "--csv", "--account", "me@x", "--last", "24h"}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 { // the header and me@x's call an hour old
		t.Fatalf("%d rows, want 2:\n%s", len(recs), b.String())
	}
}
