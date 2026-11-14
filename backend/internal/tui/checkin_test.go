package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// stubCheckin stands in for the WorkBuddy accounts signed in here: rs is
// what checking them in comes to, none signed in when rs is nil.
func stubCheckin(t *testing.T, rs []provider.WorkBuddyCheckin) *int {
	t.Helper()
	stubTrae(t, nil)
	stubQoder(t, nil)
	stubPluginCheckin(t, nil)
	oldHere, oldHas := checkinHere, hasWorkBuddy
	t.Cleanup(func() { checkinHere, hasWorkBuddy = oldHere, oldHas })
	calls := 0
	hasWorkBuddy = func() bool { return rs != nil }
	checkinHere = func(context.Context) []provider.WorkBuddyCheckin {
		calls++
		return rs
	}
	return &calls
}

// stubTrae stands in for the Trae CN accounts, as stubCheckin does
// WorkBuddy's.
func stubTrae(t *testing.T, rs []provider.WorkBuddyCheckin) *int {
	t.Helper()
	oldHere, oldHas := checkinTrae, hasTrae
	t.Cleanup(func() { checkinTrae, hasTrae = oldHere, oldHas })
	calls := 0
	hasTrae = func() bool { return rs != nil }
	checkinTrae = func(context.Context) []provider.WorkBuddyCheckin {
		calls++
		return rs
	}
	return &calls
}

func stubMiniMax(t *testing.T, rs []provider.WorkBuddyCheckin) *int {
	t.Helper()
	oldHere, oldHas := checkinMM, hasMiniMax
	t.Cleanup(func() { checkinMM, hasMiniMax = oldHere, oldHas })
	calls := 0
	hasMiniMax = func() bool { return rs != nil }
	checkinMM = func(context.Context) []provider.WorkBuddyCheckin {
		calls++
		return rs
	}
	return &calls
}

// stubPluginCheckin stands in for the accounts of plugins that check in
// themselves (auth.checkin), as stubCheckin does WorkBuddy's.
func stubPluginCheckin(t *testing.T, rs []provider.WorkBuddyCheckin) *int {
	t.Helper()
	oldHere, oldHas := checkinPl, hasPlugin
	t.Cleanup(func() { checkinPl, hasPlugin = oldHere, oldHas })
	calls := 0
	hasPlugin = func() bool { return rs != nil }
	checkinPl = func(context.Context) []provider.WorkBuddyCheckin {
		calls++
		return rs
	}
	return &calls
}

func stubQoder(t *testing.T, rs []provider.WorkBuddyCheckin) *int {
	t.Helper()
	oldHere, oldHas := checkinQd, hasQoder
	t.Cleanup(func() { checkinQd, hasQoder = oldHere, oldHas })
	calls := 0
	hasQoder = func() bool { return rs != nil }
	checkinQd = func(context.Context) []provider.WorkBuddyCheckin {
		calls++
		return rs
	}
	return &calls
}

// c on the Usage page presses WorkBuddy's daily check-in (签到) for every
// WorkBuddy (China) account signed in here at once, as the app's "Check in now" and uni-switch accounts
// checkin do, and says how each stands: the credits and streak, in already,
// or why not (akic404 on Discord: the TUI had no way to check in).
func TestTUIChecksWorkBuddyIn(t *testing.T) {
	home(t)
	usagePage := model{w: 200, h: 40, page: pageUsage}

	// nothing signed in: said so, nothing asked
	calls := stubCheckin(t, nil)
	m := press(t, usagePage, "c")
	wantFlash(t, m, false, "no WorkBuddy (China), Trae CN, MiniMax Code, Qoder or check-in plugin account is signed in")
	if *calls != 0 {
		t.Fatal("checked in with no account")
	}

	calls = stubCheckin(t, []provider.WorkBuddyCheckin{{User: "旅行者", Outcome: provider.CheckinClaimed, Credit: 100, Streak: 4, Asked: true}})
	m = press(t, usagePage, "c")
	wantFlash(t, m, true, "旅行者 checked in today +100 · a 4-day streak")
	if strings.Contains(m.flash, "already") || *calls != 1 {
		t.Fatalf("claimed now: %q, %d calls", m.flash, *calls)
	}
	if !strings.Contains(m.View(), "daily check-in") {
		t.Fatalf("the footer doesn't name c:\n%s", m.View())
	}

	// one in already today, one the vendor refused: the error, in red
	stubCheckin(t, []provider.WorkBuddyCheckin{
		{User: "旅行者", Outcome: provider.CheckinDone, Credit: 100, Streak: 4},
		{User: "second", Outcome: provider.CheckinFailed, Msg: "401: token expired"},
	})
	m = press(t, usagePage, "c")
	wantFlash(t, m, false, "旅行者 checked in today +100 · a 4-day streak · already; second couldn't check in: 401: token expired")

	stubCheckin(t, []provider.WorkBuddyCheckin{{User: "a", Outcome: provider.CheckinIneligible}, {User: "b", Outcome: provider.CheckinInactive}})
	m = press(t, usagePage, "c")
	wantFlash(t, m, true, "a isn't eligible for the check-in; b: no check-in event now")
}

// c checks the Trae CN accounts in too, after WorkBuddy's, each said as
// Trae CN's (#694); with only Trae CN signed in, it is checked in alone.
func TestTUIChecksTraeIn(t *testing.T) {
	home(t)
	usagePage := model{w: 200, h: 40, page: pageUsage}

	wb := stubCheckin(t, []provider.WorkBuddyCheckin{{User: "旅行者", Outcome: provider.CheckinDone, Credit: 100, Streak: 4}})
	tr := stubTrae(t, []provider.WorkBuddyCheckin{{User: "hu", By: "trae", Outcome: provider.CheckinClaimed, Credit: 100, Asked: true}})
	m := press(t, usagePage, "c")
	wantFlash(t, m, true, "旅行者 checked in today +100 · a 4-day streak · already; Trae CN hu checked in today +100")
	if *wb != 1 || *tr != 1 {
		t.Fatalf("calls: workbuddy %d, trae %d", *wb, *tr)
	}

	stubCheckin(t, nil)
	stubTrae(t, []provider.WorkBuddyCheckin{{User: "hu", By: "trae", Outcome: provider.CheckinIneligible, Msg: "device checked in"}})
	m = press(t, usagePage, "c")
	wantFlash(t, m, true, "Trae CN hu isn't eligible for the check-in")

	// and MiniMax Code's (#811)
	stubTrae(t, nil)
	mm := stubMiniMax(t, []provider.WorkBuddyCheckin{{User: "hu", By: "minimax", Outcome: provider.CheckinClaimed, Credit: 800, Streak: 2, Asked: true}})
	m = press(t, usagePage, "c")
	wantFlash(t, m, true, "MiniMax Code hu checked in today +800 · a 2-day streak")
	if *mm != 1 {
		t.Fatalf("minimax asked %d times", *mm)
	}

	// and Qoder's daily credits, each said as Qoder's
	stubMiniMax(t, nil)
	qd := stubQoder(t, []provider.WorkBuddyCheckin{{User: "arno", By: "qoder", Outcome: provider.CheckinClaimed, Credit: 100, Asked: true}})
	m = press(t, usagePage, "c")
	wantFlash(t, m, true, "Qoder arno checked in today +100")
	if *qd != 1 {
		t.Fatalf("qoder asked %d times", *qd)
	}
}

// A WorkBuddy (China) account's line on the Usage page says how today's
// check-in went, as its card in the app does, and points at c while it
// isn't in.
func TestCheckinOnTheUsageLine(t *testing.T) {
	now := time.Now()
	today := provider.CheckinDay(now)
	win := []provider.QuotaWindow{{Name: "Month", Used: 10}}
	qs := []provider.SubscriptionQuota{
		{Name: "WorkBuddy", User: "旅行者", Checkins: true, Windows: win, Checkin: &provider.WorkBuddyCheckin{Day: today, Outcome: provider.CheckinClaimed, Credit: 100, Streak: 4}},
		{Name: "WorkBuddy", User: "second", Checkins: true, Windows: win, Checkin: &provider.WorkBuddyCheckin{Day: "2020-01-01", Outcome: provider.CheckinClaimed}},
		{Name: "WorkBuddy", User: "third", Checkins: true, Error: "sign-in has expired", Checkin: &provider.WorkBuddyCheckin{Day: today, Outcome: provider.CheckinFailed}},
		{Name: "WorkBuddy", User: "fourth", Checkins: true, Windows: win, Checkin: &provider.WorkBuddyCheckin{Day: today, Outcome: provider.CheckinOwnApp}},
		{Name: "Trae CN", User: "fifth", Checkins: true, Windows: win, Checkin: &provider.WorkBuddyCheckin{Day: today, By: "trae", Outcome: provider.CheckinOwnApp}},
		{Name: "Codex", User: "me@example.com", Windows: win},
	}
	got := strings.Join(quotaLines(qs, true, false, 200, now), "\n")
	for _, want := range []string{"签到 ✓ +100 · 4-day streak", "second", "签到 not yet today · c", "sign-in has expired   签到 failed · c tries again", "签到 only in WorkBuddy's own app · check in there", "签到 refused by Trae CN (9074) · check in in its app"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "Codex") && strings.Contains(l, "签到") {
			t.Errorf("a check-in on a Codex line: %s", l)
		}
	}
}

// c checks in the accounts of a plugin that checks in itself (auth.checkin;
// Lemon on Discord) with the built-ins', named by the plugin's vendor; a
// captcha is said as such, not as a failure.
func TestTUIChecksPluginsIn(t *testing.T) {
	home(t)
	usagePage := model{w: 200, h: 40, page: pageUsage}
	stubCheckin(t, nil)
	calls := stubPluginCheckin(t, []provider.WorkBuddyCheckin{
		{User: "a@x", By: "plugin:fakeco", Vendor: "FakeCo", Outcome: provider.CheckinClaimed, Credit: 50, Streak: 3},
		{User: "b@x", By: "plugin:fakeco", Vendor: "FakeCo", Outcome: provider.CheckinCaptcha, Msg: "slide the puzzle"},
	})
	m := press(t, usagePage, "c")
	wantFlash(t, m, true, "FakeCo a@x")
	wantFlash(t, m, true, "FakeCo b@x asks for a captcha: check in in its own app")
	if *calls != 1 {
		t.Fatalf("checked in %d times", *calls)
	}
}

// Trae CN's 9074 says what Trae did, refused the check-in, and where it
// can still be done; it doesn't say Trae pays only its own app, which
// Buddy Switch's check-ins disprove (#1920). WorkBuddy's keeps its words.
func TestTraeOwnAppSaysTraeRefused(t *testing.T) {
	trae := checkinWords(provider.WorkBuddyCheckin{User: "hu", By: "trae", Outcome: provider.CheckinOwnApp})
	if !strings.HasSuffix(trae, ": Trae CN refused this check-in (9074: too many taking part); check in in the Trae CN app") || strings.Contains(trae, "only gives") {
		t.Errorf("Trae CN's 9074: %q", trae)
	}
	wb := checkinWords(provider.WorkBuddyCheckin{User: "hu", Outcome: provider.CheckinOwnApp})
	if !strings.Contains(wb, "WorkBuddy only gives check-in credits to its own app") {
		t.Errorf("WorkBuddy's own-app: %q", wb)
	}
}

// #3326: an "already in" uni-switch asks again (Again: before noon Beijing,
// with no claim uni-switch saw) may be the vendor's day before, so it is not
// shown as today's check-in.
func TestCheckinAgainNotShownAsToday(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 28, 0, time.FixedZone("CST", 8*3600))
	r := provider.WorkBuddyCheckin{User: "177", By: "plugin:autoclaw-cn", Vendor: "AutoClaw CN", Day: "2026-10-23", Outcome: provider.CheckinDone, Credit: 200, Again: true}
	if w := checkinWords(r); strings.Contains(w, "checked in today") || !strings.Contains(w, "asks again later") {
		t.Errorf("words: %q", w)
	}
	if c := checkinCell(provider.SubscriptionQuota{Checkins: true, Checkin: &r}, now); strings.Contains(c, "✓") || !strings.Contains(c, "asked again later") {
		t.Errorf("cell: %q", c)
	}
	r.Again = false
	if w := checkinWords(r); !strings.Contains(w, "checked in today +200") {
		t.Errorf("settled done: %q", w)
	}
	if c := checkinCell(provider.SubscriptionQuota{Checkins: true, Checkin: &r}, now); !strings.Contains(c, "✓") {
		t.Errorf("settled cell: %q", c)
	}
}
