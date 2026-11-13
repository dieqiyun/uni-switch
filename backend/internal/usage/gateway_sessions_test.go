package usage

import (
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func resetUsageTest(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	_ = provider.Path()
}

func TestGatewaySessionsRequiresExplicitIdentityAndKeepsModels(t *testing.T) {
	resetUsageTest(t)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	Append(Record{Time: at, Agent: "codex", Session: "s-1", Provider: "p", Model: "m1", Input: 10, Output: 2, Status: 200})
	Append(Record{Time: at.Add(time.Minute), Agent: "codex", Session: "s-1", Provider: "p", Model: "m2", Input: 3, Output: 4, Status: 200})
	Append(Record{Time: at, Agent: "codex", Provider: "p", Model: "m1", Input: 999, Output: 999, Status: 200})
	got := GatewaySessions(time.Time{}, nil)
	if len(got) != 1 || got[0].ID != "s-1" || len(got[0].Models) != 2 || got[0].Input != 13 || got[0].Output != 6 {
		t.Fatalf("sessions=%+v", got)
	}
}

func TestGatewaySessionsCacheRefreshAndOwnership(t *testing.T) {
	resetUsageTest(t)
	at := time.Now()
	Append(Record{Time: at, Agent: "pi", Session: "native", Provider: "p", Model: "m", Input: 10, Status: 200})
	first := GatewaySessions(time.Time{}, nil)
	if len(first) != 1 {
		t.Fatalf("first: %+v", first)
	}
	// the lists are the kept projection's, read only; an append is the
	// caller's own (#2776)
	again := GatewaySessions(time.Time{}, nil)
	if &again[0].Daily[0] != &first[0].Daily[0] {
		t.Fatal("each ask has a copy of the days of its own")
	}
	mine := append(first[0].Daily, GatewayDayUsage{Date: "mine"})
	theirs := append(again[0].Daily, GatewayDayUsage{Date: "theirs"})
	models := append(first[0].Models, GatewayModel{Model: "mine"})
	if mine[1].Date != "mine" || theirs[1].Date != "theirs" || &mine[0] == &first[0].Daily[0] || &models[0] == &first[0].Models[0] {
		t.Fatalf("two callers' appends share the kept lists: %+v %+v", mine, theirs)
	}
	if again := GatewaySessions(time.Time{}, nil); len(again[0].Models) != 1 || len(again[0].Daily) != 1 || again[0].Models[0].Model != "m" || again[0].Daily[0].Input != 10 {
		t.Fatalf("caller changed cache: %+v", again)
	}
	if got := GatewaySessions(time.Time{}, map[string]bool{"pi|native": true}); len(got) != 0 {
		t.Fatalf("native exclusion lost: %+v", got)
	}
	Append(Record{Time: at.Add(time.Second), Agent: "pi", Session: "native", Provider: "p", Model: "m", Input: 5, Status: 200})
	if got, ok := GatewaySessionByID("pi", "native", nil); !ok || got.Input != 15 {
		t.Fatalf("append not reflected: %+v %v", got, ok)
	}
}

// uni-switch makes up no session ids, so one starting "request-" is the
// client's own and is counted like any other (#1355).
func TestClientRequestPrefixedSessionsCount(t *testing.T) {
	resetUsageTest(t)
	Append(Record{Time: Clock(), Agent: "pi", Session: "request-mine", Provider: "p", Model: "m", Input: 10, Status: 200})
	summary := Summarize(All)
	if len(summary.Sessions) != 1 || summary.Sessions[0].ID != "request-mine" {
		t.Fatalf("the client's session is missing from Usage: %+v", summary.Sessions)
	}
	if got := Vias(time.Time{}); len(got.Of("pi|request-mine")) != 1 {
		t.Fatalf("the client's session is missing from Routing: %+v", got)
	}
	if _, ok := GatewaySessionByID("pi", "request-mine", nil); !ok {
		t.Fatal("the client's session is missing from Sessions")
	}
}

func TestGatewaySessionsNativeIdentityWins(t *testing.T) {
	resetUsageTest(t)
	Append(Record{Time: time.Now(), Agent: "pi", Session: "native", Provider: "p", Model: "m", Input: 1, Status: 200})
	if got := GatewaySessions(time.Time{}, map[string]bool{"pi|native": true}); len(got) != 0 {
		t.Fatalf("got duplicate gateway session: %+v", got)
	}
}

func TestGatewaySessionsCachePricesWindowsAndRewrite(t *testing.T) {
	resetUsageTest(t)
	at := time.Now()
	Append(Record{Time: at.Add(-time.Hour), Agent: "pi", Session: "native", Provider: "p", Model: "m", Input: 1000000, Status: 200})
	Append(Record{Time: at, Agent: "pi", Session: "native", Provider: "p", Model: "m", Input: 1000000, Status: 200})
	for _, price := range []float64{2, 4} {
		cfg := settings.Load()
		cfg.ModelPrices = map[string]settings.ModelPrice{"*/m": {Input: &price, Output: new(0.0), CacheRead: new(0.0), CacheWrite: new(0.0)}}
		if err := settings.Save(cfg); err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			all := GatewaySessions(time.Time{}, nil)
			window, days := GatewaySessionWindow(at.Add(-time.Minute))
			if len(all) != 1 || all[0].Cost != 2*price || len(window) != 1 || window[0].Cost != price || len(days) != 1 || days[0].Input != 1000000 {
				t.Fatalf("price=%v read=%d all=%+v window=%+v days=%+v", price, i, all, window, days)
			}
		}
	}
	vias := Vias(time.Time{})
	vias.Of("pi|native")[0].Calls = 999
	if got := Vias(time.Time{}).Of("pi|native")[0].Calls; got != 2 {
		t.Fatalf("caller mutated routing cache: %d", got)
	}
	if got := Vias(at.Add(-time.Minute)).Of("pi|native")[0].Calls; got != 1 {
		t.Fatalf("routing window: %d", got)
	}
	if err := os.WriteFile(Path(), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := GatewaySessions(time.Time{}, nil); len(got) != 0 {
		t.Fatalf("rewrite retained sessions: %+v", got)
	}
	if got := Vias(time.Time{}); got.Len() != 0 {
		t.Fatalf("rewrite retained routing: %+v", got)
	}
}

// A range of whole days is read out of the one all-time projection: no
// second copy is built or kept for it, and what it reads equals a direct
// build from that midnight (#2776).
func TestGatewayWindowsShareOneProjection(t *testing.T) {
	resetUsageTest(t)
	price := 3.0
	cfg := settings.Load()
	cfg.ModelPrices = map[string]settings.ModelPrice{"*/priced": {Input: &price, Output: &price, CacheRead: new(0.0), CacheWrite: new(0.0)}}
	if err := settings.Save(cfg); err != nil {
		t.Fatal(err)
	}
	today := time.Now()
	day := func(back int) time.Time {
		y, m, d := today.AddDate(0, 0, -back).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	for back := 5; back >= 0; back-- {
		at := day(back).Add(10 * time.Hour)
		Append(Record{Time: at, Agent: "pi", Session: "long", Provider: "p", Model: "priced", Input: 1000 * (back + 1), Output: 7, Millis: 1500, Status: 200})
		Append(Record{Time: at.Add(time.Minute), Agent: "pi", Session: "long", Provider: "p", Model: "free", Input: 11, Output: 3, CacheRead: 5, Status: 200})
		if back%2 == 0 {
			Append(Record{Time: at.Add(2 * time.Minute), Agent: "codex", Session: "even", Provider: "p", Model: "priced", Input: 300, Output: 9, Millis: 250, Status: 200})
		}
	}
	Append(Record{Time: day(5).Add(time.Hour), Agent: "pi", Session: "old", Provider: "p", Model: "priced", Input: 9, Status: 200})

	key := func(ss []GatewaySession) map[string]GatewaySession {
		m := map[string]GatewaySession{}
		for _, s := range ss {
			m[s.Agent+"|"+s.ID] = s
		}
		return m
	}
	backs := []int{0, 1, 2, 3, 7}
	base := gatewayBuilds.Load()
	GatewaySessions(time.Time{}, nil)
	got := map[int][]GatewaySession{}
	for _, back := range backs {
		got[back] = GatewaySessions(day(back), nil)
	}
	if built := gatewayBuilds.Load() - base; built != 1 {
		t.Fatalf("built %d projections for one log and %d ranges, want 1 kept and shared by them all", built, len(backs))
	}
	for _, back := range backs {
		want, _ := buildGatewaySessions(readLogSnapshot(), day(back), nil, nil)
		g, w := key(got[back]), key(want)
		if len(g) != len(w) {
			t.Fatalf("from %d days back: %d sessions, want %d: %+v", back, len(g), len(w), got[back])
		}
		for k, ws := range w {
			gs, ok := g[k]
			if !ok {
				t.Fatalf("from %d days back: %s missing", back, k)
			}
			if !gs.Start.Equal(ws.Start) || !gs.Last.Equal(ws.Last) || gs.Tokens != ws.Tokens || gs.Unpriced != ws.Unpriced || math.Abs(gs.Cost-ws.Cost) > 1e-9 || len(gs.Models) != len(ws.Models) || len(gs.Daily) != len(ws.Daily) {
				t.Fatalf("from %d days back, %s:\n got %+v\nwant %+v", back, k, gs, ws)
			}
			for i := range ws.Models {
				a, b := gs.Models[i], ws.Models[i]
				if a.Model != b.Model || a.Tokens != b.Tokens || a.Priced != b.Priced || math.Abs(a.Cost-b.Cost) > 1e-9 {
					t.Fatalf("from %d days back, %s model %d: got %+v want %+v", back, k, i, a, b)
				}
			}
		}
	}
}
