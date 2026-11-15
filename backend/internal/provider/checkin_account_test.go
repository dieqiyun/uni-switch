package provider

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Check in now for one account (xiaowu_95660: 手动签到一次) asks that
// account alone, once, even one left out of the daily check-in, and
// changes no switch: the other account is neither asked nor said, a
// second press the same day is answered from the claim, and the left-out
// account stays left out.
func TestCheckinOneAccountNow(t *testing.T) {
	signIn(t)
	f := &fakeWBCheckin{active: true}
	srv := f.serve(t)
	now := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	w := wbCheckinFixture(t, srv, &now)
	one := w.accounts()[0]
	two := one
	two.User, two.creds.UID = "second", "u2"
	w.accounts = func() []wbAccount { return []wbAccount{one, two} }
	ctx := context.Background()
	leaveOut(t, "workbuddy|u2")

	c := w.checkiner()
	c.only = "workbuddy|u2"
	rs := c.checkinNow(ctx, true)
	if len(rs) != 1 || rs[0].User != "second" || rs[0].Outcome != CheckinClaimed || !rs[0].Asked {
		t.Fatalf("pressed for the left-out account: %+v", rs)
	}
	s0, cl0 := f.counts()
	if cl0 != 1 {
		t.Fatalf("claims %d, want 1 (the other account asked too?)", cl0)
	}
	if off := settings.Load().CheckinOff; !slices.Equal(off, []string{"workbuddy|u2"}) {
		t.Fatalf("the press changed the switch: %v", off)
	}

	// pressed again the same day: the claim stands, the vendor not asked
	rs = c.checkinNow(ctx, true)
	if len(rs) != 1 || rs[0].Asked || !rs[0].Checked() {
		t.Fatalf("second press: %+v", rs)
	}
	if s, cl := f.counts(); s != s0 || cl != cl0 {
		t.Fatalf("second press asked: status %d, claims %d", s, cl)
	}

	// the daily loop still leaves it out and checks the other in
	f.mu.Lock()
	f.checked = false
	f.mu.Unlock()
	rs = w.checkinNow(ctx, false)
	if len(rs) != 1 || rs[0].User != "旅行者" || !rs[0].Asked {
		t.Fatalf("the loop after the press: %+v", rs)
	}
}

// A press for an account no check-in reaches answers nothing.
func TestCheckinAccountNowUnknown(t *testing.T) {
	signIn(t)
	if rs := CheckInAccount(context.Background(), "", "workbuddy|nobody"); len(rs) != 0 {
		t.Fatalf("unknown account: %+v", rs)
	}
	if rs := CheckInAccount(context.Background(), "", ""); len(rs) != 0 {
		t.Fatalf("no key: %+v", rs)
	}
}
