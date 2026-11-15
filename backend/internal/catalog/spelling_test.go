package catalog

import (
	"strings"
	"testing"
)

// spellingCatalog is models.dev's rows for the models of xyenon's report
// (Discord #feedback, 10-29), as models.dev/api.json had them on 10-29 and
// trimmed to their limits, levels and prices, beside near misses: Ark's
// pay-as-you-go list says kimi-k3-1-pro-261024 for what Moonshot and the
// relays list as kimi-k3.1-pro, and Venice as kimi-k3-1-pro.
const spellingCatalog = `{
  "moonshotai": {"models": {
    "kimi-k3.1-pro": {"id":"kimi-k3.1-pro","name":"Kimi K3.1 Pro","limit":{"context":1048576,"output":1048576},
      "reasoning_options":[{"type":"effort","values":["low","high"]}],
      "cost":{"input":2,"output":10,"cache_read":0.2,"cache_write":2}},
    "kimi-k3.1-pro-highspeed": {"id":"kimi-k3.1-pro-highspeed","limit":{"context":1048576,"output":1048576},
      "cost":{"input":4,"output":20,"cache_read":0.2,"cache_write":4}},
    "kimi-k3": {"id":"kimi-k3","limit":{"context":1048576,"output":131072},
      "cost":{"input":3,"output":15,"cache_read":0.3,"cache_write":3}}
  }},
  "vercel": {"models": {
    "moonshotai/kimi-k3.1-pro": {"id":"moonshotai/kimi-k3.1-pro","limit":{"context":1048576,"output":1048576},
      "cost":{"input":2,"output":10,"cache_read":0.2,"cache_write":2}}
  }},
  "venice": {"models": {
    "kimi-k3-1-pro": {"id":"kimi-k3-1-pro","limit":{"context":1000000,"output":131072},
      "cost":{"input":3,"output":15,"cache_read":0.3}}
  }},
  "near-miss": {"models": {
    "kimi-k3.1-pro-max": {"id":"kimi-k3.1-pro-max","limit":{"context":524288,"output":65536}},
    "kimi-k3.1-mini": {"id":"kimi-k3.1-mini","limit":{"context":262144,"output":32768}},
    "kimi-k3.2-pro": {"id":"kimi-k3.2-pro","limit":{"context":2097152,"output":262144}}
  }},
  "volcengine": {"models": {
    "glm-5-3-flash-260828": {"id":"glm-5-3-flash-260828","limit":{"context":1000000,"output":131072},
      "cost":{"input":0.11875,"output":0.41563,"cache_read":0.03414}}
  }},
  "volcengine-coding-plan": {"models": {
    "glm-5.3-flash": {"id":"glm-5.3-flash","name":"GLM-5.3-Flash","limit":{"context":1000000,"output":131072},
      "reasoning_options":[{"type":"effort","values":["low","high","max"]}],
      "cost":{"input":0,"output":0,"cache_read":0}},
    "kimi-k3": {"id":"kimi-k3","limit":{"context":1048576,"output":131072},
      "cost":{"input":0,"output":0,"cache_read":0}}
  }}
}`

// A model models.dev lists under another spelling alone has its window,
// reply limit and levels: the one its spelling (SameModel) folds into, as
// most entries spell it. A different model (-pro-max, -mini, another
// version, a four-digit release) folds into nothing it lists.
func TestOtherSpellingLimits(t *testing.T) {
	writeCatalog(t, spellingCatalog)
	for _, c := range []struct {
		id          string
		window, out int
	}{
		// the report: Ark's id for kimi-k3.1-pro
		{"kimi-k3-1-pro-261024", 1048576, 1048576},
		{"volcengine/kimi-k3-1-pro-261024", 1048576, 1048576},
		{"Kimi-K3.1-Pro-20261024", 1048576, 1048576},
		// listed as given: Venice's own word, not the other spelling's
		{"kimi-k3-1-pro", 1000000, 131072},
		{"kimi-k3.1-pro", 1048576, 1048576},
		// near misses keep their own, or none
		{"kimi-k3-1-pro-max-261024", 524288, 65536},
		{"kimi-k3-1-mini-261024", 262144, 32768},
		{"kimi-k3-1-pro-mini-261024", 0, 0},
		{"kimi-k3-2-pro-261024", 2097152, 262144},
		{"kimi-k3-3-pro-261024", 0, 0},
		{"kimi-k3-pro-261024", 0, 0},
		{"kimi-k3-1-261024", 0, 0},
		{"kimi-k3-1-pro-highspeed-261024", 1048576, 1048576},
		{"kimi-k3-1-pro-2610", 0, 0},   // a four-digit release is a model of its own
		{"kimi-k3-1-pro-261324", 0, 0}, // no month 13: not a date
		{"kimi-k3-1-pro-fast-261024", 0, 0},
	} {
		if got := ContextOf(c.id); got != c.window {
			t.Errorf("ContextOf(%q) = %d, want %d", c.id, got, c.window)
		}
		if got := OutputOf(c.id); got != c.out {
			t.Errorf("OutputOf(%q) = %d, want %d", c.id, got, c.out)
		}
	}
	if got := strings.Join(EffortsOf("kimi-k3-1-pro-261024"), ","); got != "low,high" {
		t.Errorf("EffortsOf = %q, want low,high", got)
	}
	if !Knows("kimi-k3-1-pro-261024") {
		t.Error("Knows: the other spelling is a model models.dev knows")
	}
	// the usage report sends only an id models.dev knows as written
	if KnowsAsWritten("kimi-k3-1-pro-261024") || !KnowsAsWritten("kimi-k3.1-pro") || !KnowsAsWritten("kimi-k3-1-pro") {
		t.Error("KnowsAsWritten took another spelling")
	}
	// the id itself is kept: only what is known of it is borrowed
	if got := NameOf("kimi-k3-1-pro-261024"); got != "" {
		t.Errorf("NameOf = %q: the other spelling's name stands for a dated id", got)
	}
}

// A price is never borrowed by another spelling: neither the maker's nor
// another host's prices Ark's dated id.
func TestOtherSpellingIsNotPriced(t *testing.T) {
	writeCatalog(t, spellingCatalog)
	for _, ps := range [][]string{{"volcengine"}, {"moonshotai"}, {"volcengine", "moonshotai", "vercel", "venice"}} {
		if pr, ok := PricedBy(ps, "kimi-k3-1-pro-261024"); ok {
			t.Errorf("PricedBy(%v) = %+v", ps, pr)
		}
	}
	if pr, ok := PricedBy([]string{"volcengine-coding-plan"}, "glm-5-3-flash-260828"); ok {
		t.Errorf("the plan's price taken for pay-as-you-go's id: %+v", pr)
	}
}

// The provider's own catalog answers first: Ark's pay-as-you-go list says
// glm-5-3-flash-260828 where its plan's catalog says glm-5.3-flash. What
// the vendor's list says itself stays, and no price comes with it.
func TestDecorateOtherSpelling(t *testing.T) {
	writeCatalog(t, spellingCatalog)
	known := Provider("volcengine-coding-plan")
	out := Decorate([]Model{
		{ID: "glm-5-3-flash-260828", Context: 200000},
		{ID: "glm-5-3-flash-max-260828"},
		{ID: "kimi-k3-1-pro-261024"},
	}, known)
	g := out[0]
	if g.ID != "glm-5-3-flash-260828" || g.Context != 200000 || g.Output != 131072 || g.Price != nil {
		t.Errorf("glm: %+v", g)
	}
	if strings.Join(g.Efforts, ",") != "low,high,max" || g.Name != "GLM-5.3-Flash" {
		t.Errorf("glm levels/name: %v %q", g.Efforts, g.Name)
	}
	if m := out[1]; m.Context != 0 || m.Output != 0 || len(m.Efforts) > 0 {
		t.Errorf("a -max variant took glm-5.3-flash's: %+v", m)
	}
	// the plan doesn't list kimi-k3.1-pro, and kimi-k3 is another model
	if m := out[2]; m.Context != 0 || m.Output != 0 {
		t.Errorf("kimi-k3.1-pro took kimi-k3's: %+v", m)
	}
	if got, ok := ListedBy([]string{"volcengine-coding-plan"}, "glm-5-3-flash-260828"); !ok || strings.Join(got, ",") != "low,high,max" {
		t.Errorf("ListedBy = %v %v", got, ok)
	}
	if _, ok := ListedBy([]string{"volcengine-coding-plan"}, "kimi-k3-1-pro-261024"); ok {
		t.Error("ListedBy took kimi-k3 for kimi-k3.1-pro")
	}
}

// SameModel folds what the report's ids differ by, and nothing that names
// another model.
func TestSameModelReportIDs(t *testing.T) {
	k := SameModel("kimi-k3.1-pro")
	for _, id := range []string{"kimi-k3-1-pro-261024", "kimi-k3-1-pro", "moonshotai/Kimi-K3.1-Pro", "kimi_k3_1_pro"} {
		if SameModel(id) != k {
			t.Errorf("SameModel(%q) = %q, want %q", id, SameModel(id), k)
		}
	}
	for _, id := range []string{"kimi-k3.1-pro-max", "kimi-k3.1-mini", "kimi-k3.2-pro", "kimi-k3-pro", "kimi-k3.1-pro-2610", "kimi-k31-pro"} {
		if SameModel(id) == k {
			t.Errorf("SameModel(%q) folds into kimi-k3.1-pro", id)
		}
	}
}
