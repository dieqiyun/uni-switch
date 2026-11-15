package provider

import (
	"context"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestAccountWarmupSchedulesStaySeparate(t *testing.T) {
	azureHome(t)
	f := &fakeWarm{now: at8(28, 6, 0), errs: map[string]error{}, ws: map[string][]QuotaWindow{}}
	for _, u := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		f.ws[u] = []QuotaWindow{win("5 hours", fiveHours, 0, f.now.Add(fiveHours))}
	}
	w := f.warmer(filepath.Join(t.TempDir(), "warmup.json"))
	w.accountPrefs = func() map[string]settings.AccountWarmup {
		return map[string]settings.AccountWarmup{
			"a@example.com": {Times: []string{"09:00", "15:00"}},
			"c@example.com": {},
		}
	}
	w.warmNow(context.Background(), "", []string{"06:00"})
	if !reflect.DeepEqual(f.sent, []string{"b@example.com"}) {
		t.Fatalf("shared schedule affected overridden accounts: %v", f.sent)
	}
	f.sent = nil
	f.now = at8(28, 9, 0)
	for u := range f.ws {
		f.ws[u] = []QuotaWindow{win("5 hours", fiveHours, 0, f.now.Add(fiveHours))}
	}
	w.warmNow(context.Background(), "", nil)
	if !reflect.DeepEqual(f.sent, []string{"a@example.com"}) {
		t.Fatalf("own schedule with global off: %v", f.sent)
	}
	f.sent = nil
	f.now = at8(29, 15, 0)
	for u := range f.ws {
		f.ws[u] = []QuotaWindow{win("5 hours", fiveHours, 0, f.now.Add(fiveHours))}
	}
	w.warmNow(context.Background(), "", nil)
	if !reflect.DeepEqual(f.sent, []string{"a@example.com"}) {
		t.Fatalf("second own time: %v", f.sent)
	}
}
func TestAccountCheckinScheduleDoesNotEnableOtherAccounts(t *testing.T) {
	azureHome(t)
	s := settings.Load()
	s.AccountCheckins = map[string]bool{"workbuddy|a": true, "workbuddy|c": false}
	s.WorkBuddyCheckin = false
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	sent := []string{}
	c := checkiner{path: filepath.Join(t.TempDir(), "checkins.json"), now: func() time.Time { return time.Date(2026, 11, 9, 1, 0, 0, 0, time.UTC) }, mu: &sync.Mutex{}, scheduled: true}
	c.accounts = func() []checkinAcct {
		out := []checkinAcct{}
		for _, u := range []string{"a", "b", "c"} {
			out = append(out, checkinAcct{User: u, On: true, key: "workbuddy|" + u, do: func(context.Context) WorkBuddyCheckin {
				sent = append(sent, u)
				return WorkBuddyCheckin{User: u, Outcome: CheckinClaimed}
			}})
		}
		return out
	}
	c.checkinNow(context.Background(), false)
	if !reflect.DeepEqual(sent, []string{"a"}) {
		t.Fatalf("unexpected scheduled accounts: %v", sent)
	}
	if !accountCheckinEnabled("workbuddy") || accountCheckinEnabled("trae") {
		t.Fatal("schedule enabled for wrong vendor")
	}
	// A manual action still reaches an explicitly disabled account.
	c.only = "workbuddy|c"
	c.checkinNow(context.Background(), true)
	if !reflect.DeepEqual(sent, []string{"a", "c"}) {
		t.Fatalf("manual check-in: %v", sent)
	}
}
