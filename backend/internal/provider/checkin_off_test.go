package provider

import (
	"context"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// leaveOut saves keys as the accounts left out of their daily check-in.
func leaveOut(t *testing.T, keys ...string) {
	t.Helper()
	s := settings.Load()
	s.CheckinOff = keys
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// An account the user left out of the daily check-in (#1902, magicLaLa:
// 根据每个账号设置是否开启自动签到) is asked nothing, by the loop or a
// press of Check in now, while its vendor's check-in is on; turned back
// on, it is checked in at once.
func TestCheckinAccountLeftOut(t *testing.T) {
	signIn(t)
	f := &fakeWBCheckin{active: true}
	srv := f.serve(t)
	now := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	ctx := context.Background()

	leaveOut(t, "workbuddy|u1")
	for _, soon := range []bool{false, true} {
		if rs := c.checkinNow(ctx, soon); len(rs) != 0 {
			t.Fatalf("left out, soon %v: %+v", soon, rs)
		}
	}
	if s, cl := f.counts(); s != 0 || cl != 0 {
		t.Fatalf("left out, yet asked: status %d, claims %d", s, cl)
	}

	// another account's entry leaves this one in
	leaveOut(t, "workbuddy|u2")
	rs := c.checkinNow(ctx, false)
	if len(rs) != 1 || rs[0].Outcome != CheckinClaimed || !rs[0].Asked {
		t.Fatalf("back on: %+v", rs)
	}
}
