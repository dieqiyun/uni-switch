package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// plugins#304: an inactive or ineligible check-in says the plugin's (or
// vendor's) reason beside its label; one without a reason reads as before.
func TestCheckinSaysWhyNoneWasOffered(t *testing.T) {
	const why = "Qoder check-in: Qoder offered this device's identity no daily credits campaign right now"
	old := checkinAll
	t.Cleanup(func() { checkinAll = old })
	checkinAll = func(context.Context) []provider.WorkBuddyCheckin {
		return []provider.WorkBuddyCheckin{
			{User: "arno", By: "plugin:qoder", Vendor: "Qoder", Outcome: provider.CheckinInactive, Msg: why},
			{User: "lei", By: "trae", Outcome: provider.CheckinIneligible, Msg: "this device has checked in today"},
			{User: "ann", By: "plugin:qoder", Vendor: "Qoder", Outcome: provider.CheckinInactive},
			{User: "bo", By: "plugin:qoder", Vendor: "Qoder", Outcome: provider.CheckinInactive, Msg: why, Again: true},
		}
	}
	out, err := stdoutOf(t, func() error { return checkinWorkBuddy(false) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Qoder arno no check-in event now · " + why,
		"Trae CN lei not eligible for the check-in · this device has checked in today",
		"Qoder bo no check-in event now · " + why + " · uni-switch asks again later",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Qoder ann") && !strings.HasSuffix(strings.TrimSpace(l), "ann no check-in event now") {
			t.Errorf("no reason, as before: %q", l)
		}
	}
}
