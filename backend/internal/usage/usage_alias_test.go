package usage

import (
	"math"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A model counted under another id (uni-switch model alias) is one row in the
// usage summary and in a gateway session, and each call keeps the price of
// the id it was made under: the alias's own price is never put on the
// other id's calls (SlouchyQuill507 on Discord: deepseek-v4.1-flash and
// deepseek-flash).
func TestUsageAliasCountsTwoIdsAsOne(t *testing.T) {
	resetUsageTest(t)
	cfg := settings.Load()
	cfg.ModelPrices = map[string]settings.ModelPrice{
		"*/deepseek-v4.1-flash": {Input: new(2.0), Output: new(0.0), CacheRead: new(0.0), CacheWrite: new(0.0)},
		"*/deepseek-flash":      {Input: new(100.0), Output: new(0.0), CacheRead: new(0.0), CacheWrite: new(0.0)},
	}
	if err := settings.Save(cfg); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute)
	Append(Record{Time: at, Agent: "pi", Session: "s", Provider: "p", Model: "deepseek-v4.1-flash", Input: 1000000, Status: 200})
	Append(Record{Time: at, Agent: "pi", Session: "s", Provider: "p", Model: "deepseek-flash", Input: 1000000, Status: 200})

	// off by default: two rows, as before
	if s := Summarize(All); len(s.Models) != 2 {
		t.Fatalf("with no alias: %+v", s.Models)
	}
	if got := GatewaySessions(time.Time{}, nil); len(got) != 1 || len(got[0].Models) != 2 {
		t.Fatalf("with no alias: %+v", got)
	}

	if _, err := provider.SetUsageAlias("deepseek-v4.1-flash", "deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	s := Summarize(All)
	if len(s.Models) != 1 || s.Models[0].ID != "p/deepseek-flash" || s.Models[0].Model != "deepseek-flash" || s.Models[0].Input != 2000000 {
		t.Fatalf("aliased summary: %+v", s.Models)
	}
	// $2 for the v4.1 call at its own price, $100 for the other: not $200
	if c := s.Models[0].Cost; math.Abs(c-102) > 1e-9 || math.Abs(s.Cost-102) > 1e-9 {
		t.Fatalf("aliased cost %v (total %v), want 102: each call at its own id's price", c, s.Cost)
	}
	got := GatewaySessions(time.Time{}, nil)
	if len(got) != 1 || len(got[0].Models) != 1 || got[0].Models[0].Model != "deepseek-flash" || got[0].Models[0].Input != 2000000 {
		t.Fatalf("aliased session: %+v", got)
	}
	if m := got[0].Models[0]; !m.Priced || math.Abs(m.Cost-102) > 1e-9 {
		t.Fatalf("aliased session cost: %+v", m)
	}
	if _, days := GatewaySessionWindow(time.Time{}); len(days) != 1 || days[0].Model != "deepseek-flash" || math.Abs(days[0].Cost-102) > 1e-9 {
		t.Fatalf("aliased days: %+v", days)
	}

	// --reset: two rows again
	if _, err := provider.SetUsageAlias("deepseek-v4.1-flash", ""); err != nil {
		t.Fatal(err)
	}
	if s := Summarize(All); len(s.Models) != 2 {
		t.Fatalf("after reset: %+v", s.Models)
	}
}

// An unpriced id counted under a priced one leaves the merged row unpriced:
// the alias's price is not borrowed for it.
func TestUsageAliasKeepsUnpricedCallsUnpriced(t *testing.T) {
	resetUsageTest(t)
	cfg := settings.Load()
	cfg.ModelPrices = map[string]settings.ModelPrice{
		"*/known-model": {Input: new(5.0), Output: new(0.0), CacheRead: new(0.0), CacheWrite: new(0.0)},
	}
	if err := settings.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.SetUsageAlias("zz-unknown-model-x", "known-model"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute)
	Append(Record{Time: at, Agent: "pi", Session: "s", Provider: "p", Model: "zz-unknown-model-x", Input: 1000000, Status: 200})
	Append(Record{Time: at, Agent: "pi", Session: "s", Provider: "p", Model: "known-model", Input: 1000000, Status: 200})
	got := GatewaySessions(time.Time{}, nil)
	if len(got) != 1 || len(got[0].Models) != 1 {
		t.Fatalf("session: %+v", got)
	}
	if m := got[0].Models[0]; m.Priced || math.Abs(m.Cost-5) > 1e-9 {
		t.Fatalf("merged row %+v: want unpriced, $5 from the priced id only", m)
	}
	s := Summarize(All)
	if len(s.Models) != 1 || math.Abs(s.Models[0].Cost-5) > 1e-9 || s.Models[0].Unpriced != 1 {
		t.Fatalf("summary: %+v", s.Models)
	}
}
