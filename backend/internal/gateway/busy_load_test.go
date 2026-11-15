package gateway

import (
	"net/http"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func busyLoadAt(t *testing.T, at int) {
	t.Helper()
	s := settings.Load()
	was := s.BusyLoad
	s.BusyLoad = at
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s := settings.Load()
		s.BusyLoad = was
		settings.Save(s)
	})
}

// A conversation a model answered that has since gone busy goes to the
// ready one that leads at its next turn, whatever it cached; within a
// turn it stays, on the reasoning it sealed. A turn going round, or a new
// conversation spread, never lands on the busy one past a ready one.
func TestBusyModelNotKeptAtNewTurn(t *testing.T) {
	p := provider.PluginForTest("inhouse-plugin", "inhouse")
	cs := []candidate{{p: p, model: "mid", rest: "inhouse-plugin/mid"}, {p: p, model: "big", rest: "inhouse-plugin/big"}}
	pl := planned{order: []Weighed{{Who: "mid"}, {Who: "big", Busy: 92}}}
	h := http.Header{}
	h.Set("x-claude-code-session-id", "busy-load-kept")
	turn := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	within := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}]}`)
	_, _, _, key := affine("t", provider.AffinitySession, false, false, false, h, provider.Chat, turn, turn, cs, pl)
	answered(key, cs[1], 1, Usage{CacheRead: 50_000})
	t.Cleanup(func() { sticks.Lock(); delete(sticks.m, key); sticks.Unlock() })

	got, _, aff, _ := affine("t", provider.AffinitySession, false, false, false, h, provider.Chat, within, within, cs, pl)
	if got[0].rest != "inhouse-plugin/big" || !aff.Kept {
		t.Fatalf("within a turn: first %s, kept %v, why %s", got[0].rest, aff.Kept, aff.Why)
	}
	got, _, aff, _ = affine("t", provider.AffinitySession, false, false, false, h, provider.Chat, turn, turn, cs, pl)
	if got[0].rest != "inhouse-plugin/mid" || aff.Kept || aff.Why != "busy" {
		t.Fatalf("a new turn: first %s, kept %v, why %s", got[0].rest, aff.Kept, aff.Why)
	}

	// every one busy: kept where it was
	both := planned{order: []Weighed{{Who: "mid", Busy: 85}, {Who: "big", Busy: 92}}}
	got, _, aff, _ = affine("t", provider.AffinitySession, false, false, false, h, provider.Chat, turn, turn, cs, both)
	if got[0].rest != "inhouse-plugin/big" || !aff.Kept {
		t.Fatalf("all busy: first %s, kept %v, why %s", got[0].rest, aff.Kept, aff.Why)
	}

	if got, _ := after(append([]candidate(nil), cs...), planned{order: append([]Weighed(nil), pl.order...)}, 0); got[0].rest != "inhouse-plugin/mid" {
		t.Fatalf("turn went to %s", got[0].rest)
	}
	if readyForNew(pl.order, 1) {
		t.Fatal("a new conversation may go to the busy one")
	}
}
