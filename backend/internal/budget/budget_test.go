package budget

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// A call inside one of its provider's rate windows (#2463) costs its rate
// against a gateway key's cost limit, so the same tokens spend the cap in
// fewer calls than outside the window: the records are priced at their
// own time, by the same Pricer the Usage page prices with.
func TestCostLimitCountsTheRateWindow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	// $1000 a million input: 300 tokens of input a call costs $0.30
	// outside the window, $0.90 inside it
	in, zero := 1000.0, 0.0
	if err := settings.Save(settings.Settings{ModelPrices: map[string]settings.ModelPrice{
		"plan/m1": {Input: &in, Output: &zero, CacheRead: &zero, CacheWrite: &zero},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Key: "k", Chat: "https://plan.example/v1",
		RateWindows: []provider.RateWindow{{Start: "10:00", End: "14:00", Rate: 3}}}); err != nil {
		t.Fatal(err)
	}
	newKey := func(name string) access.Key {
		t.Helper()
		if _, err := access.Update("add-key", access.Change{Name: name}); err != nil {
			t.Fatal(err)
		}
		ks, err := access.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range ks {
			if k.Name == name {
				if _, err := access.Update("limit-key", access.Change{Key: k.ID, Limit: &access.Limit{Period: "day", Cost: 1.0}}); err != nil {
					t.Fatal(err)
				}
				ks, err := access.List()
				if err != nil {
					t.Fatal(err)
				}
				for _, k := range ks {
					if k.Name == name {
						return k
					}
				}
			}
		}
		t.Fatal("the key is not there")
		return access.Key{}
	}
	day := time.Now()
	at := func(h, m int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local)
	}
	spent := func(k access.Key, when time.Time) bool {
		t.Helper()
		who := access.Identity{KeyID: k.ID, KeyName: k.Name, Limit: k.Limit, More: k.More}
		_, refused := Reserve(who, -1, "", when)
		return refused != nil
	}
	call := func(k access.Key, h, m int) {
		t.Helper()
		Append(usage.Record{Time: at(h, m), Provider: "plan", Model: "m1", Input: 300, Status: 200, CallerKeyID: k.ID})
	}

	// two calls at the peak's 3×, $0.90 each: $1.80 of a $1 cap, the next
	// is out; the same two off the peak are $0.60 and leave room
	peak := newKey("Peak")
	call(peak, 11, 0)
	call(peak, 11, 30)
	if !spent(peak, at(11, 40)) {
		t.Fatal("a key over $1 of peak calls is let in")
	}
	off := newKey("Off")
	call(off, 15, 0)
	call(off, 15, 10)
	if spent(off, at(15, 20)) {
		t.Fatal("two off-peak calls spent a $1 cap")
	}
	call(off, 15, 30)
	if spent(off, at(15, 40)) {
		t.Fatal("three off-peak calls, $0.90 of a $1 cap, are spent")
	}
	call(off, 15, 50)
	if !spent(off, at(16, 0)) {
		t.Fatal("four off-peak calls, $1.20 of a $1 cap, are let in")
	}
}
