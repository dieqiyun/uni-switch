package agent

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uniSwitchModels answers from the list it built while nothing the catalog
// is built from changed: the window's drift poll asked each agent's Drift
// for its list again per field every 15s, each built whole with its
// prices and Vision's ranking (14.5 GB of 33 GB a live uni-switch allocated
// in 53 minutes). A provider or a setting saved since is seen at once.
func TestUniSwitchModelsKeptWhileCatalogStays(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5", "gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	ids := func() map[string]bool {
		out := map[string]bool{}
		for _, m := range uniSwitchModels("pi") {
			out[m.ID] = true
		}
		return out
	}
	if got := ids(); !got["oa/gpt-5.5"] || !got["oa/gpt-5.4"] {
		t.Fatalf("oa's models not listed: %v", got)
	}
	built := testing.AllocsPerRun(5, func() { buildModels("pi") })
	kept := testing.AllocsPerRun(20, func() { uniSwitchModels("pi") })
	// StampNow's look at three files and the copy; a list built is far more
	if kept > 200 || kept*10 > built {
		t.Fatalf("a list already built took %.0f allocations, building it %.0f", kept, built)
	}

	// each caller's copy is its own: sorting or writing one changes nothing
	// the next caller is given
	mine := uniSwitchModels("pi")
	mine[0].ID = "mine"
	for _, m := range uniSwitchModels("pi") {
		if m.ID == "mine" {
			t.Fatalf("a caller's write reached the list kept: %+v", m)
		}
	}

	if err := provider.Save(provider.Provider{ID: "nb", Name: "NB", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if !ids()["nb/x"] {
		t.Fatal("a provider saved since is not listed")
	}
	s := settings.Load()
	s.HiddenModels = map[string][]string{"pi": {"oa/gpt-5.4"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if ids()["oa/gpt-5.4"] {
		t.Fatal("a model hidden from pi since is still listed")
	}
}

// viauni-switch, every agent's model picker, is kept the same way: Claude
// Code's options alone (claudeViauni-switch) were 4.7 GB of the 33 GB.
func TestViaUniSwitchKeptWhileCatalogStays(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5", "gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	refs := func(prefix string) map[string]bool {
		out := map[string]bool{}
		for _, o := range viaUniSwitch("pi", prefix) {
			out[o.Value] = true
		}
		return out
	}
	if got := refs(""); !got["oa/gpt-5.5"] || !got["oa/gpt-5.4"] {
		t.Fatalf("oa's models not offered: %v", got)
	}
	// a prefix is kept apart from the bare list
	if got := refs("uni-switch/"); !got["uni-switch/oa/gpt-5.5"] {
		t.Fatalf("the prefixed list: %v", got)
	}
	built := testing.AllocsPerRun(5, func() { buildVia("pi", "") })
	kept := testing.AllocsPerRun(20, func() { viaUniSwitch("pi", "") })
	if kept > 100 || kept*5 > built {
		t.Fatalf("options already built took %.0f allocations, building them %.0f", kept, built)
	}
	mine := viaUniSwitch("pi", "")
	for i := range mine {
		mine[i].Value += "[1m]"
	}
	if got := refs(""); !got["oa/gpt-5.5"] {
		t.Fatalf("a caller's write reached the options kept: %v", got)
	}
	if err := provider.Save(provider.Provider{ID: "nb", Name: "NB", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if !refs("")["nb/x"] {
		t.Fatal("a provider saved since is not offered")
	}
	s := settings.Load()
	s.HiddenModels = map[string][]string{"pi": {"oa/gpt-5.4"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if refs("")["oa/gpt-5.4"] {
		t.Fatal("a model hidden from pi since is still offered")
	}
}
