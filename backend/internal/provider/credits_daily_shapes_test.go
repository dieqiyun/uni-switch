package provider

import (
	"encoding/json"

	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func jsonNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

// TestDailyCreditsKeptBefore: WorkBuddy's days, kept by provider and
// account alone before #3321, are still its card's, and counting goes on
// from its last reading under the window's own name.
func TestDailyCreditsKeptBefore(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.Local) }
	old := `{
  "workbuddy|李雷": {
    "since": "2026-09-20",
    "last": {"used": 130.25, "total": 5600, "at": "2026-10-01T23:00:00+08:00"},
    "days": {"2026-09-20": 12.5, "2026-10-01": 30.25}
  }
}`
	if err := os.MkdirAll(filepath.Dir(creditsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creditsPath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	card := wbCard("workbuddy", "李雷", 130.25, 5600)
	want := &DailyCredits{Since: "2026-09-20", Days: []DayCredits{{"2026-09-20", 12.5}, {"2026-10-01", 30.25}}, Window: "Credits"}
	if d := withDailyCredits([]SubscriptionQuota{card}, day(2, 9))[0].Daily; !reflect.DeepEqual(d, want) {
		t.Errorf("before a new reading: %+v, want %+v", d, want)
	}
	noteDailyCredits([]SubscriptionQuota{wbCard("workbuddy", "李雷", 140.25, 5600)}, day(2, 10))
	want.Days = append(want.Days, DayCredits{"2026-10-02", 10})
	if d := withDailyCredits([]SubscriptionQuota{card}, day(2, 11))[0].Daily; !reflect.DeepEqual(d, want) {
		t.Errorf("after: %+v, want %+v", d, want)
	}
	b, _ := os.ReadFile(creditsPath())
	var kept map[string]json.RawMessage
	if err := json.Unmarshal(b, &kept); err != nil || len(kept) != 1 || kept["workbuddy|李雷|Credits"] == nil {
		t.Errorf("the file: %s", b)
	}
}
