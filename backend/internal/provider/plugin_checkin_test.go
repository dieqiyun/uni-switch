package provider

import (
	"context"

	"os"
	"path/filepath"

	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// pressed is the keys the plugin's check-in was pressed for, in order.
func pressed(t *testing.T, file string) []string {
	t.Helper()
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}

// pluginCheckinOn turns fakeco's daily check-in on.
func pluginCheckinOn(t *testing.T) {
	t.Helper()
	s := settings.Load()
	s.PluginCheckins = map[string]bool{"fakeco": true}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// pluginCards are a card for each of p's accounts, as the Usage page has.
func pluginCards(p pluginCheckinProvider) []SubscriptionQuota {
	var qs []SubscriptionQuota
	for _, a := range p.accounts {
		qs = append(qs, SubscriptionQuota{Provider: PluginID(p.ID), User: a.User})
	}
	return qs
}

// userOf is the name the account signed in under key is shown by.
func userOf(p pluginCheckinProvider, key string) string {
	for _, a := range p.accounts {
		if a.key == pluginCheckinKey(p.ID, key) {
			return a.User
		}
	}
	return ""
}

// A plugin-checkin.json that doesn't parse is unknown: nothing is carried
// over or written over, and it is carried once it reads.
func TestPluginCheckinCarriesNothingOverAnUnreadableFile(t *testing.T) {
	path := filepath.Join(claudeHome(t), "plugin-checkin.json")
	order, heirs := []string{"fakeco|A"}, map[string][]string{"fakeco|A": {"fakeco|fakeco", "fakeco|fakeco#x1"}}
	bad := []byte(`{"fakeco|A": {"day": "2026-10-06",`)
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if adoptLegacyCheckinFile(path, order, heirs) {
		t.Fatal("a file that doesn't parse was taken as carried over")
	}
	if b, _ := os.ReadFile(path); string(b) != string(bad) {
		t.Fatalf("the file was written over: %s", b)
	}
	if err := os.WriteFile(path, []byte(`{"fakeco|A": {"day": "2026-10-06", "outcome": "claimed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !adoptLegacyCheckinFile(path, order, heirs) {
		t.Fatal("not carried once the file reads")
	}
	st := shownCheckins(path)
	if _, ok := st["fakeco|fakeco"]; !ok || len(st) != 1 {
		t.Fatalf("carried as %+v", st)
	}
}

// pluginCheckinCounter is a plugin provider with one account whose
// check-in answers what *outcome holds and counts the asks in *calls.
func pluginCheckinCounter(id string, outcome *string, calls *int) pluginCheckinProvider {
	return pluginCheckinProvider{ID: id, accounts: []pluginCheckinAcct{{User: "ann", On: true, key: id + "|ann", card: id, do: func(context.Context) WorkBuddyCheckin {
		*calls++
		return WorkBuddyCheckin{Outcome: *outcome}
	}}}}
}

// #2696 (LoKi, mufanmu): the Qoder plugins' check-in is rechecked as
// uni-switch's own Qoder check-in is. A campaign can open mid-day, so a done or
// inactive read before it was kept for the whole Beijing day, even on a
// press. Now the loop asks again 30 minutes after the last answer, not
// before, and a press asks at once. ZCode's gift plans are rechecked the
// same way (plugins#503).
func TestPluginCheckinQoderRechecksCampaigns(t *testing.T) {
	for _, id := range []string{"qoder", QoderCNID, "zcode"} {
		for _, first := range []string{CheckinInactive, CheckinDone} {
			// each: wait this long, press or not, then the outcome shown
			// and the asks so far
			type step struct {
				wait      time.Duration
				press     bool
				want      string
				wantCalls int
			}
			for name, steps := range map[string][]step{
				"loop": {
					{0, false, first, 1},
					{29 * time.Minute, false, first, 1},
					{time.Minute, false, CheckinClaimed, 2},
					{29 * time.Minute, false, CheckinClaimed, 2},
					{time.Minute, false, CheckinClaimed, 3}, // a claim is rechecked too
				},
				"press": {
					{0, false, first, 1},
					{10 * time.Minute, true, CheckinClaimed, 2},
					{0, false, CheckinClaimed, 2}, // the loop keeps what the press read
					{0, true, CheckinClaimed, 3},
				},
			} {
				t.Run(id+"/"+first+"/"+name, func(t *testing.T) {
					claudeHome(t)
					now := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC) // 09:00 Beijing
					outcome, calls := first, 0
					p := pluginCheckinCounter(id, &outcome, &calls)
					for i, s := range steps {
						now = now.Add(s.wait)
						got := p.checkiner(func() time.Time { return now }).checkinNow(context.Background(), s.press)
						if len(got) != 1 || got[0].Outcome != s.want || calls != s.wantCalls {
							t.Fatalf("step %d (press=%v at %s): got %+v after %d asks; want %s after %d", i, s.press, now.Format(time.TimeOnly), got, calls, s.want, s.wantCalls)
						}
						outcome = CheckinClaimed
					}
				})
			}
		}
	}
}

// plugins#503 (MiniFullC): a ZCode account whose claim asked for the
// app's captcha was asked again every 30 minutes, the claim posted and
// answered 3007 each time, six more times in one evening. A captcha, or
// an own-app answer, waits on the user, so the loop leaves it for the
// day under a recheck too; a press still asks, and the next day asks.
func TestPluginCheckinRecheckLeavesCaptcha(t *testing.T) {
	for _, id := range []string{"zcode", "qoder"} {
		for _, first := range []string{CheckinCaptcha, CheckinOwnApp} {
			t.Run(id+"/"+first, func(t *testing.T) {
				claudeHome(t)
				now := time.Date(2026, 11, 3, 14, 16, 0, 0, time.UTC) // 22:16 Beijing
				outcome, calls := first, 0
				p := pluginCheckinCounter(id, &outcome, &calls)
				run := func(press bool) WorkBuddyCheckin {
					t.Helper()
					got := p.checkiner(func() time.Time { return now }).checkinNow(context.Background(), press)
					if len(got) != 1 {
						t.Fatalf("at %s: %+v", now, got)
					}
					return got[0]
				}
				run(false)
				outcome = CheckinClaimed
				for _, step := range []time.Duration{31 * time.Minute, 31 * time.Minute, 30 * time.Minute} {
					now = now.Add(step)
					if r := run(false); r.Outcome != first || calls != 1 {
						t.Fatalf("loop at %s: %+v after %d asks; want %s kept after 1", now.In(beijing).Format(time.TimeOnly), r, calls, first)
					}
				}
				if r := run(true); r.Outcome != CheckinClaimed || calls != 2 {
					t.Fatalf("press: %+v after %d asks", r, calls)
				}
				outcome = first
				now = time.Date(2026, 11, 4, 14, 0, 0, 0, time.UTC) // the next Beijing day
				run(false)
				if calls != 3 {
					t.Fatalf("the next day: %d asks, want 3", calls)
				}
			})
		}
	}
}

// Every other plugin's check-in is settled for the Beijing day by a claim,
// or by a done, inactive or ineligible answered from noon Beijing on: the
// loop doesn't ask again after 30 minutes (#2696 changes Qoder's, plugins#503 ZCode's).
// A press asks again for any of those but a claim: the record is not the
// vendor's answer now (#3326).
func TestPluginCheckinOthersSettledForTheDay(t *testing.T) {
	for _, first := range []string{CheckinInactive, CheckinDone, CheckinIneligible, CheckinClaimed} {
		t.Run(first, func(t *testing.T) {
			claudeHome(t)
			now := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC) // 12:00 Beijing
			outcome, calls := first, 0
			p := pluginCheckinCounter("fakeco", &outcome, &calls)
			p.checkiner(func() time.Time { return now }).checkinNow(context.Background(), false)
			outcome = CheckinClaimed
			for _, step := range []time.Duration{31 * time.Minute, 5 * time.Hour} {
				now = now.Add(step)
				got := p.checkiner(func() time.Time { return now }).checkinNow(context.Background(), false)
				if len(got) != 1 || got[0].Outcome != first || got[0].Again || calls != 1 {
					t.Fatalf("loop at %s: got %+v after %d asks; want %s kept after 1", now, got, calls, first)
				}
			}
			got := p.checkiner(func() time.Time { return now }).checkinNow(context.Background(), true)
			want, wantCalls := CheckinClaimed, 2
			if first == CheckinClaimed {
				wantCalls = 1
			}
			if len(got) != 1 || got[0].Outcome != want || calls != wantCalls {
				t.Fatalf("press at %s: got %+v after %d asks; want %s after %d", now, got, calls, want, wantCalls)
			}
		})
	}
}

// #3326 (connoryang331): at 00:00:28 Beijing AutoClaw CN, WorkBuddy,
// CodeArts, Trae CN, MiniMax and Raccoon answered "already in" (done) for
// accounts whose day's credits were still to claim; uni-switch kept it as the
// day's, the loop never asked again, and a press answered from the record.
// An answer uni-switch saw no claim for, given before noon Beijing, is asked
// again every 30 minutes; a press asks at once.
func TestPluginCheckinEarlyDoneAskedAgain(t *testing.T) {
	early := time.Date(2026, 10, 23, 0, 0, 28, 0, beijing)
	run := func(p pluginCheckinProvider, now time.Time, press bool) WorkBuddyCheckin {
		t.Helper()
		got := p.checkiner(func() time.Time { return now }).checkinNow(context.Background(), press)
		if len(got) != 1 {
			t.Fatalf("at %s: %+v", now, got)
		}
		return got[0]
	}
	t.Run("loop", func(t *testing.T) {
		claudeHome(t)
		outcome, calls := CheckinDone, 0
		p := pluginCheckinCounter("autoclaw-cn", &outcome, &calls)
		now := early
		if r := run(p, now, false); r.Outcome != CheckinDone || !r.Again || calls != 1 {
			t.Fatalf("at midnight: %+v after %d asks", r, calls)
		}
		// shown so on the card too, and in Settings
		if r := shownCheckins(pluginCheckinPath())["autoclaw-cn|ann"]; !r.Again {
			t.Fatalf("kept: %+v", r)
		}
		if r := run(p, now.Add(29*time.Minute), false); r.Outcome != CheckinDone || !r.Again || calls != 1 {
			t.Fatalf("29 minutes on: %+v after %d asks", r, calls)
		}
		now = now.Add(wbCheckinEvery)
		if r := run(p, now, false); r.Outcome != CheckinDone || !r.Again || calls != 2 {
			t.Fatalf("30 minutes on: %+v after %d asks", r, calls)
		}
		// the vendor's day turns
		outcome = CheckinClaimed
		now = now.Add(wbCheckinEvery)
		if r := run(p, now, false); r.Outcome != CheckinClaimed || r.Again || calls != 3 {
			t.Fatalf("after the vendor's turn: %+v after %d asks", r, calls)
		}
		// a claim settles the day, for the loop and a press
		for _, step := range []time.Duration{wbCheckinEvery, 5 * time.Hour} {
			now = now.Add(step)
			for _, press := range []bool{false, true} {
				if r := run(p, now, press); r.Outcome != CheckinClaimed || calls != 3 {
					t.Fatalf("press=%v at %s: %+v after %d asks", press, now, r, calls)
				}
			}
		}
	})
	t.Run("press", func(t *testing.T) {
		claudeHome(t)
		outcome, calls := CheckinDone, 0
		p := pluginCheckinCounter("autoclaw-cn", &outcome, &calls)
		run(p, early, false)
		outcome = CheckinClaimed
		if r := run(p, time.Date(2026, 10, 23, 10, 54, 0, 0, beijing), true); r.Outcome != CheckinClaimed || !r.Asked || calls != 2 {
			t.Fatalf("pressed at 10:54: %+v after %d asks", r, calls)
		}
	})
	t.Run("done all day", func(t *testing.T) {
		claudeHome(t)
		outcome, calls := CheckinDone, 0
		p := pluginCheckinCounter("autoclaw-cn", &outcome, &calls)
		var last WorkBuddyCheckin
		for now := early; wbCheckinDay(now) == "2026-10-23"; now = now.Add(wbCheckinEvery) {
			last = run(p, now, false)
		}
		// 00:00:28 to 11:30:28 provisional, 12:00:28 the day's
		if calls != 25 || last.Again || last.Outcome != CheckinDone {
			t.Fatalf("%d asks, last %+v", calls, last)
		}
	})
	t.Run("done after uni-switch's claim", func(t *testing.T) {
		claudeHome(t)
		outcome, calls := CheckinClaimed, 0
		// Qoder's is rechecked after a claim, and then answers done
		p := pluginCheckinCounter("qoder", &outcome, &calls)
		run(p, early, false)
		outcome = CheckinDone
		if r := run(p, early.Add(wbCheckinEvery), false); r.Outcome != CheckinDone || r.Again || r.Claimed.IsZero() || calls != 2 {
			t.Fatalf("done after the claim: %+v after %d asks", r, calls)
		}
	})
}
