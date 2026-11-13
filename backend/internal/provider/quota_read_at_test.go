package provider

import (
	"context"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// A Claude account's allowance is what Claude Code last told: shown again
// on each refresh, it says when it was told (ReadAt), and the history
// keeps it at that time, once, rather than as a new reading each time it
// is shown (#802).
func TestClaudeKeptReadingKeepsItsTime(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	told := time.Now().Add(-40 * time.Minute).Truncate(time.Second)
	reset := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	claudeUsage.Lock()
	was := claudeUsage.m
	claudeUsage.m = map[string]claudeUsageEntry{"kept@example.com": {at: told, heard: told,
		ws: []QuotaWindow{{Name: "5 hours", Used: 30, ResetsAt: &reset}}}}
	claudeUsage.Unlock()
	t.Cleanup(func() { claudeUsage.Lock(); claudeUsage.m = was; claudeUsage.Unlock() })

	saved := Login{Agent: "claude", User: "Kept@example.com"}
	var q SubscriptionQuota
	for i := range 3 {
		q = readNow(loginQuota(context.Background(), saved))
		q.User = saved.User
		if q.Error != "" || len(q.Windows) != 1 {
			t.Fatalf("saved account's card = %+v", q)
		}
		if q.ReadAt == nil || !q.ReadAt.Equal(told) {
			t.Fatalf("read at %v, want when Claude Code told it, %v", q.ReadAt, told)
		}
		noteQuotaHistory([]SubscriptionQuota{q}, time.Now().Add(time.Duration(i)*10*time.Minute))
	}
	pts := lineOf(t, QuotaHistories(told.Add(-time.Hour), "", ""), "kept@example.com", "5 hours")
	if len(pts) != 1 || !pts[0].At.Equal(told.UTC()) || pts[0].Left != 70 {
		t.Fatalf("history = %+v, want one point at %v", pts, told)
	}
	if r := quotaReport([]SubscriptionQuota{q}, nil, nil, time.Now()); len(r) != 1 || r[0].ReadAt == nil || !r[0].ReadAt.Equal(told) {
		t.Fatalf("report = %+v", r)
	}

	// a reading just made is stamped now; a failed one or one kept from
	// before isn't
	if q := readNow(SubscriptionQuota{Provider: "codex", Windows: []QuotaWindow{{Name: "5 hours", Used: 1}}}); q.ReadAt == nil || time.Since(*q.ReadAt) > time.Minute {
		t.Errorf("a new reading: %+v", q)
	}
	if q := readNow(SubscriptionQuota{Provider: "codex", Error: "401"}); q.ReadAt != nil {
		t.Errorf("a failed reading was stamped: %+v", q)
	}
	if q := readNow(SubscriptionQuota{Provider: "codex", AsOf: &told, Windows: []QuotaWindow{{Name: "5 hours"}}}); q.ReadAt != nil {
		t.Errorf("a kept reading was stamped: %+v", q)
	}
}
