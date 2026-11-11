package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Code's uni-switch list is kept as viauni-switch is: its model and tier
// fields asked it each poll, for the picker and again for Drift, built
// anew each time (30 MB of forty polls with real accounts). Each caller
// still has a copy of its own, and a provider saved since is offered.
func TestClaudeViaUniSwitchKept(t *testing.T) {
	syncHome(t)
	models := []string{"claude-opus-5-5", "gpt-5.4"}
	for i := range 100 {
		models = append(models, "m"+strconv.Itoa(i))
	}
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	values := func() map[string]bool {
		out := map[string]bool{}
		for _, o := range claudeViaUniSwitch(false) {
			out[o.Value] = true
		}
		return out
	}
	if got := values(); !got["oa/gpt-5.4"] {
		t.Fatalf("oa's models not offered: %v", got)
	}
	built := testing.AllocsPerRun(5, func() { buildClaudeVia(false) })
	kept := testing.AllocsPerRun(20, func() { claudeViaUniSwitch(false) })
	if kept > 20 || kept*5 > built {
		t.Fatalf("Claude Code's options already built took %.0f allocations, building them %.0f", kept, built)
	}
	mine := claudeViaUniSwitch(false)
	for i := range mine {
		mine[i].Value = "x"
	}
	if got := values(); !got["oa/gpt-5.4"] {
		t.Fatalf("a caller's write reached the options kept: %v", got)
	}
	// the model field joins the list kept to its own models: its
	// options are never the kept list itself, even with no own models
	home := os.Getenv("HOME")
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{}`)
	field := claude(home).Field("model").Options(map[string]string{})
	if !slices.ContainsFunc(field, func(o Option) bool { return o.Value == "oa/gpt-5.4" }) {
		t.Fatal("the model field doesn't offer oa's models")
	}
	for i := range field {
		field[i].Value = "x"
	}
	for _, fold := range []bool{false, true} {
		if slices.ContainsFunc(claudeViaUniSwitch(fold), func(o Option) bool { return o.Value == "x" }) {
			t.Fatalf("a write to the model field's options reached the options kept (folded %v)", fold)
		}
	}
	// folded and not are kept apart
	for _, o := range claudeViaUniSwitch(true) {
		if o.Value == "x" {
			t.Fatal("the folded list is the other one")
		}
	}
	if err := provider.Save(provider.Provider{ID: "nb", Name: "NB", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"y"}}); err != nil {
		t.Fatal(err)
	}
	if !values()["nb/y"] {
		t.Fatal("a provider saved since is not offered")
	}
}
