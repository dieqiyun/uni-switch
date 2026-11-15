package provider

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// threeAccounts signs agent in to three accounts in a sandbox HOME, all on
// in uni-switch, and answers them as `next` takes them: the two the agent
// isn't on, from the one after the account it is on in their order,
// wrapping round, then that account.
func threeAccounts(t *testing.T, agent string) []string {
	t.Helper()
	users := pickUpThree(t, agent)
	for _, u := range users {
		if err := SetLoginOn(agent, u, true); err != nil {
			t.Fatal(err)
		}
	}
	return users
}

// pickUpThree has agent sign itself in to three accounts in turn in a
// sandbox HOME, uni-switch picking each up, and answers them as threeAccounts
// does, on or off as uni-switch left them.
func pickUpThree(t *testing.T, agent string) []string {
	t.Helper()
	if agent == "codex" {
		home := signIn(t) // me@example.com
		rememberLogins(true)
		codexSignIn(t, home, "work@example.com", "r-work")
		rememberLogins(true)
		codexSignIn(t, home, "third@example.com", "r-third")
		rememberLogins(true)
	} else {
		claudeThree(t)
	}
	ls := Logins(agent)
	at := slices.IndexFunc(ls, func(l Login) bool { return l.Active })
	if len(ls) != 3 || at < 0 {
		t.Fatalf("not three accounts, one signed in: %+v", ls)
	}
	if at == 2 {
		t.Fatalf("the one signed in is the last, so nothing wraps round: %+v", ls)
	}
	var users []string
	for i := range ls {
		users = append(users, ls[(at+1+i)%3].User)
	}
	return users
}

func usedAt(agent string, used map[string]float64) func(context.Context, string) map[string]SubscriptionQuota {
	at := time.Now().Add(time.Hour)
	return func(context.Context, string) map[string]SubscriptionQuota {
		out := map[string]SubscriptionQuota{}
		for u, v := range used {
			out[u] = SubscriptionQuota{Provider: agent, User: u, Windows: []QuotaWindow{
				{Name: "5 hours", Used: v, ResetsAt: &at, Span: 5 * time.Hour},
				{Name: "Fable", Used: 100, Model: "fable", ResetsAt: &at}, // one model's own: never counted
			}}
		}
		return out
	}
}

// StringKe (#4532): when the account the agent is on is spent and every
// other is past its line too, the switch moves to the one with the most
// room left, as cswap does, and stays when none has more than the one it
// is on by roomMargin. Codex shares the switch, so it is held to it too.
func TestNextLoginTakesTheMostRoomWhenAllArePast(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			u := threeAccounts(t, agent)
			on := u[2]
			t.Cleanup(func() { LoginUsageVia(nil) })
			for _, c := range []struct {
				name string
				used []float64 // u[0], u[1], on
				to   string
			}{
				{"the first has room", []float64{50, 99, 100}, u[0]},
				{"the second has the most room", []float64{99.5, 98.5, 100}, u[1]},
				{"the first has the most room", []float64{98.2, 99.9, 100}, u[0]},
				{"none has more by the margin", []float64{99.5, 99.5, 100}, ""},
				{"the one it is on has the most", []float64{100, 100, 99}, ""},
			} {
				LoginUsageVia(usedAt(agent, map[string]float64{u[0]: c.used[0], u[1]: c.used[1], on: c.used[2]}))
				from, to, back, ok := NextLogin(context.Background(), agent)
				if c.to == "" {
					if ok {
						t.Errorf("%s: moved %q→%q, want it to stay", c.name, from, to)
					}
					continue
				}
				if !ok || back || from != on || to != c.to {
					t.Errorf("%s: got %q→%q back=%v ok=%v, want %q→%q", c.name, from, to, back, ok, on, c.to)
				}
			}
		})
	}
}

// `uni-switch accounts switch <agent> next` takes the accounts in order from
// the one after the account the agent is on, wrapping round: the first
// with room, else the one with the most room left, said to be past its
// line. One off in uni-switch is taken as one on is (#4532).
func TestNextInOrder(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			u := threeAccounts(t, agent)
			on := u[2]
			t.Cleanup(func() { LoginUsageVia(nil) })
			for _, c := range []struct {
				name string
				used []float64
				off  int // 1-based index of an account turned off, 0 none
				to   string
				over bool
			}{
				{"the one after it", []float64{10, 10, 10}, 0, u[0], false},
				{"the first spent, the second has room", []float64{99, 10, 10}, 0, u[1], false},
				{"every one past, the second has the most", []float64{99.5, 98.5, 10}, 0, u[1], true},
				{"every one past, the first has the most", []float64{98.5, 100, 10}, 0, u[0], true},
				{"the first off", []float64{10, 10, 10}, 1, u[0], false},
				{"the second off, the first spent", []float64{99, 10, 10}, 2, u[1], false},
			} {
				LoginUsageVia(usedAt(agent, map[string]float64{u[0]: c.used[0], u[1]: c.used[1], on: c.used[2]}))
				if c.off > 0 {
					if err := SetLoginOn(agent, u[c.off-1], false); err != nil {
						t.Fatal(err)
					}
				}
				to, over, err := NextInOrder(context.Background(), agent)
				if err != nil || !strings.EqualFold(to, c.to) || over != c.over {
					t.Errorf("%s: got %q over=%v err=%v, want %q over=%v", c.name, to, over, err, c.to, c.over)
				}
				if c.off > 0 {
					if err := SetLoginOn(agent, u[c.off-1], true); err != nil {
						t.Fatal(err)
					}
				}
			}
			// none read: uni-switch says so rather than guess
			LoginUsageVia(func(context.Context, string) map[string]SubscriptionQuota { return map[string]SubscriptionQuota{} })
			if to, _, err := NextInOrder(context.Background(), agent); err == nil {
				t.Errorf("no readings: got %q, want an error", to)
			}
		})
	}
	if _, _, err := NextInOrder(context.Background(), "gemini"); err == nil {
		t.Error("next for Gemini CLI: want an error")
	}
}

// StringKe (#4532): an account the agent signed itself in to (Claude
// Code's /login, codex login) starts off in uni-switch, and `switch next`
// passed over it, or said there was no other account when every one came
// that way. next takes the accounts the agent can be signed in to, on or
// off, as switching by name does; a lapsed one stays out.
func TestNextTakesAccountsThatAreOff(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			u := pickUpThree(t, agent)
			for _, l := range Logins(agent) {
				if l.On && !l.Active {
					t.Fatalf("%s, which %s signed in to itself, is on: the case this test is about is gone", l.User, agent)
				}
			}
			t.Cleanup(func() { LoginUsageVia(nil) })
			LoginUsageVia(usedAt(agent, map[string]float64{u[0]: 10, u[1]: 10, u[2]: 10}))
			if to, over, err := NextInOrder(context.Background(), agent); err != nil || over || !strings.EqualFold(to, u[0]) {
				t.Fatalf("next: got %q over=%v err=%v, want %q", to, over, err, u[0])
			}
			LoginUsageVia(usedAt(agent, map[string]float64{u[0]: 99, u[1]: 10, u[2]: 10}))
			if to, _, err := NextInOrder(context.Background(), agent); err != nil || !strings.EqualFold(to, u[1]) {
				t.Fatalf("next with the first spent: got %q err=%v, want %q", to, err, u[1])
			}
			// the automatic switch still takes only accounts that are on
			LoginUsageVia(usedAt(agent, map[string]float64{u[0]: 10, u[1]: 10, u[2]: 100}))
			if from, to, _, ok := NextLogin(context.Background(), agent); ok {
				t.Errorf("automatic switch moved %q→%q to an account that is off", from, to)
			}
		})
	}
}
