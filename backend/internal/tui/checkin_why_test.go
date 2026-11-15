package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// plugins#304: an inactive or ineligible check-in says the plugin's (or
// vendor's) reason after c, and its account line points to c for it; one
// without a reason reads as before.
func TestCheckinSaysWhyNoneWasOffered(t *testing.T) {
	const why = "Qoder check-in: Qoder offered this device's identity no daily credits campaign right now"
	now := time.Date(2026, 10, 24, 15, 0, 0, 0, time.FixedZone("CST", 8*3600))
	day := provider.CheckinDay(now)
	r := provider.WorkBuddyCheckin{User: "arno", By: "plugin:qoder", Vendor: "Qoder", Day: day, Outcome: provider.CheckinInactive, Msg: why}
	if w := checkinWords(r); w != "Qoder arno: no check-in event now · "+why {
		t.Errorf("inactive: %q", w)
	}
	if c := checkinCell(provider.SubscriptionQuota{Checkins: true, Checkin: &r}, now); !strings.Contains(c, "no event now · c says why") {
		t.Errorf("inactive cell: %q", c)
	}
	n := provider.WorkBuddyCheckin{User: "lei", By: "trae", Day: day, Outcome: provider.CheckinIneligible, Msg: "this device has checked in today"}
	if w := checkinWords(n); w != "Trae CN lei isn't eligible for the check-in · this device has checked in today" {
		t.Errorf("ineligible: %q", w)
	}
	a := r
	a.Again = true
	if w := checkinWords(a); !strings.HasSuffix(w, "uni-switch asks again later · "+why) {
		t.Errorf("again: %q", w)
	}
	r.Msg = ""
	if w := checkinWords(r); w != "Qoder arno: no check-in event now" {
		t.Errorf("no reason: %q", w)
	}
	if c := checkinCell(provider.SubscriptionQuota{Checkins: true, Checkin: &r}, now); strings.Contains(c, "why") {
		t.Errorf("no reason cell: %q", c)
	}
}
