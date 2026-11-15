package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
)

// connor_yang on Discord: three sessions on a provider with three accounts,
// Stays: Session, Smart routing, ended up on two accounts. A conversation
// nobody has answered yet went where routing put it first, whatever other
// sessions were on, so two sessions shared one account while another sat
// unused. Kept for the whole session, a new session now goes to the one the
// fewest others are on (spreadsSessions, leastHeld), and each then stays
// where it began; its subagents go with it (withLead).
func TestWholeSessionSpreadsNewSessions(t *testing.T) {
	for _, mode := range []string{provider.AffinitySession, provider.AffinityStrict} {
		t.Run(mode, func(t *testing.T) {
			fresh(t)
			up := &keyed{}
			serveOn(t, "sp", "k1", []string{"m1"}, up, "k2", "k3")
			if err := provider.SetAffinity("sp", mode); err != nil {
				t.Fatal(err)
			}
			routes.Forget("sp")
			s := New()
			post := func(session, sub, msgs string) string {
				t.Helper()
				n := len(up.tried)
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"sp/m1","messages":[`+msgs+`]}`))
				req.Header.Set("x-session-id", session)
				if sub != "" {
					req.Header.Set("x-claude-code-agent-id", sub)
				}
				s.Handler().ServeHTTP(rec, req)
				if rec.Code != 200 || len(up.tried) != n+1 {
					t.Fatalf("%s: %d %s, tried %v", session, rec.Code, rec.Body.String(), up.tried[n:])
				}
				return up.tried[n]
			}
			on := map[string]string{}
			seen := map[string]string{}
			for _, ses := range []string{"s1", "s2", "s3"} {
				k := post(ses, "", `{"role":"user","content":"start `+ses+`"}`)
				if other, ok := seen[k]; ok {
					t.Errorf("%s went to %s, which %s is on; a third account had none (%v)", ses, k, other, on)
				}
				seen[k], on[ses] = ses, k
			}
			// each stays where it began, turn after turn, as before
			for _, ses := range []string{"s1", "s2", "s3"} {
				msgs := `{"role":"user","content":"start ` + ses + `"},{"role":"assistant","content":"ok"},{"role":"user","content":"more"}`
				if k := post(ses, "", msgs); k != on[ses] {
					t.Errorf("%s's next turn went to %s, not %s where it began", ses, k, on[ses])
				}
			}
			// a subagent of s2 goes with s2, not to the account fewest are on
			if k := post("s2", "sub-1", `{"role":"user","content":"a subagent's task"}`); k != on["s2"] {
				t.Errorf("s2's subagent went to %s, not s2's %s", k, on["s2"])
			}
			// a fourth session goes to one of the three, each holding one
			post("s4", "", `{"role":"user","content":"start s4"}`)
		})
	}
}

// In order is the user's own order: every new session goes to the first
// account until it is used up or fails, Stays: Session or not.
func TestInOrderKeepsNewSessionsOnTheFirst(t *testing.T) {
	fresh(t)
	up := &keyed{}
	serveOn(t, "so", "k1", []string{"m1"}, up, "k2", "k3")
	if err := provider.SetAffinity("so", provider.AffinitySession); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetRouting("so", provider.Ordered); err != nil {
		t.Fatal(err)
	}
	routes.Forget("so")
	s := New()
	for _, ses := range []string{"s1", "s2", "s3"} {
		if code, out := postAs(t, s, ses, `{"model":"so/m1","messages":[{"role":"user","content":"start `+ses+`"}]}`); code != 200 {
			t.Fatalf("%d %s", code, out)
		}
	}
	for _, k := range up.tried {
		if k != "k1" {
			t.Fatalf("In order sent a new session to %s: %v", k, up.tried)
		}
	}
}

func TestSpreadsSessionsOnlyWhenKeptAndRoutedByWhatIsLeft(t *testing.T) {
	for _, c := range []struct {
		mode, routing string
		want          bool
	}{
		{provider.AffinitySession, "", true},
		{provider.AffinityStrict, "", true},
		{provider.AffinitySession, provider.LeastUsed, true},
		{provider.AffinitySession, provider.Pace, true},
		{"", "", false},
		{provider.AffinityTurn, "", false},
		{provider.AffinityOff, "", false},
		{provider.AffinitySession, provider.Ordered, false},
		{provider.AffinitySession, provider.Weighted, false},
		{provider.AffinitySession, provider.FreeFirst, false},
		{provider.AffinitySession, provider.Cheapest, false},
		{provider.AffinitySession, provider.Rotate, false}, // spreads on its own
	} {
		if got := spreadsSessions(c.mode, c.routing); got != c.want {
			t.Errorf("spreadsSessions(%q, %q) = %v, want %v", c.mode, c.routing, got, c.want)
		}
	}
}

// Spreading a whole session's new conversations never puts one on an
// account Smart keeps new conversations off — used past lowShare — nor on a
// provider's fallback while the provider's own model leads: those stay
// behind, and with every other account holding as many sessions, routing's
// first takes it.
func TestWholeSessionSpreadLeavesTheNearlyUsedAndFallbacks(t *testing.T) {
	fresh(t)
	key := func(k string) candidate {
		p := provider.Provider{ID: "sx", Key: k}
		return candidate{p: p, model: "m1", rest: "sx#" + k}
	}
	for _, c := range []struct {
		name string
		last Weighed
	}{
		{"used past lowShare", Weighed{Known: true, Used: lowShare + 5}},
		{"a fallback", Weighed{Fallback: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			scope := "sx/m1:" + c.name
			cs := []candidate{key("k1"), key("k2"), key("k3")}
			sticks.Lock()
			for i, ses := range []string{"a", "b"} {
				sticks.m[scope+"|"+ses] = stick{rest: cs[i].rest, who: cs[i].who(), model: "m1", at: time.Now()}
			}
			sticks.Unlock()
			t.Cleanup(func() {
				sticks.Lock()
				for k := range sticks.m {
					if strings.HasPrefix(k, scope+"|") {
						delete(sticks.m, k)
					}
				}
				for k := range sticks.opening {
					if strings.HasPrefix(k, scope+"|") {
						delete(sticks.opening, k)
					}
				}
				sticks.Unlock()
			})
			pl := planned{order: []Weighed{{}, {}, c.last}}
			got, _ := leastHeld(scope, scope+"|c", cs, pl, true)
			if got[0].who() != cs[0].who() {
				t.Errorf("a new session went to %s, %s", got[0].who(), c.name)
			}
			// in turn spreads to one used past lowShare as it did, but
			// never to a fallback while the provider's own model leads
			// (standsIn; 01huadalang on Discord, 10-31): then to the one
			// of the provider's the fewest are on, k2 (k1 holds a and c)
			want := cs[2]
			if c.last.Fallback {
				want = cs[1]
			}
			got, _ = leastHeld(scope, scope+"|d", cs, planned{order: []Weighed{{}, {}, c.last}}, false)
			if got[0].who() != want.who() {
				t.Errorf("in turn, a new conversation went to %s, want %s", got[0].who(), want.who())
			}
		})
	}
}
