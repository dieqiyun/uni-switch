package provider

import (
	"context"
	"strings"
	"testing"
)

// hxy920830 on X: "use an account to 98%, switch, then keep using the
// next account to 98% — not switch right back as soon as the other
// account resets". Where the sign-in alone spends the accounts (Claude
// Code; Codex whose sign-in alone uni-switch switches), uni-switch stays on the
// account it moved to till that is spent too, and the one it moved off
// isn't said to come first again. In order still goes back to the first,
// as its note says; Codex through uni-switch goes back too
// (TestCodexSwitchedBackWhenItHasRoom), its sign-in following Smart.
func TestSignInStaysOnTheAccountMovedTo(t *testing.T) {
	for _, c := range []struct {
		name, agent string
		set         func(agent string) error
		back        bool
	}{
		{"claude", "claude", nil, false},
		{"claude switch only", "claude", func(a string) error { return SetSwitchOnly(a, true) }, false},
		{"codex switch only", "codex", func(a string) error { return SetSwitchOnly(a, true) }, false},
		{"claude in order", "claude", func(a string) error { return SetRouting(a, Ordered) }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := threeAccounts(t, c.agent)
			first := u[2] // signed in to it now
			if c.set != nil {
				if err := c.set(c.agent); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { LoginUsageVia(nil) })
			used := map[string]float64{first: 100, u[0]: 10, u[1]: 0}
			switched := func() string {
				t.Helper()
				LoginUsageVia(usedAt(c.agent, used))
				to, err := SwitchWhenSpent(context.Background(), c.agent)
				if err != nil {
					t.Fatal(err)
				}
				return to
			}
			returns := func(user string) bool {
				for _, l := range Logins(c.agent) {
					if strings.EqualFold(l.User, user) {
						return l.Returns
					}
				}
				t.Fatalf("no %s", user)
				return false
			}
			a := switched()
			if !strings.EqualFold(a, u[0]) && !strings.EqualFold(a, u[1]) {
				t.Fatalf("the spent first moved to %q", a)
			}
			if returns(first) != c.back {
				t.Fatalf("the first said to come first again: %v, want %v", returns(first), c.back)
			}
			// the first's window renews while a is half used
			used[first], used[a] = 0, 50
			to := switched()
			if c.back {
				if !strings.EqualFold(to, first) {
					t.Fatalf("in order: moved to %q, want back to %s", to, first)
				}
				return
			}
			if to != "" {
				t.Fatalf("moved off %s at 50%% to %s as the first renewed", a, to)
			}
			// a is spent in turn: on to the next with room, the first
			// among them in their order
			used[a] = 99
			if to := switched(); to == "" || strings.EqualFold(to, a) {
				t.Fatalf("a spent: moved to %q", to)
			}
		})
	}
}
