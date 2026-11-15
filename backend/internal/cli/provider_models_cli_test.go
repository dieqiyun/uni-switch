package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// uni-switch provider models <id> +m adds a model to those exposed and -m takes
// one out, keeping the rest; the whole-list form still replaces the list,
// and says it did and how many there were before (MOMO on Discord: 35
// exposed became 8 with nothing said).
func TestProviderModelsAddsRemovesAndSaysWhatItReplaced(t *testing.T) {
	groupsHome(t)
	catalog.Changed = nil
	if err := provider.Save(provider.Provider{ID: "og", Name: "OG", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{"m1", "m2", "m3"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(catalog.LivePath("og")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.LivePath("og"), []byte(`{"models":[{"id":"m1"},{"id":"m2"},{"id":"m3"},{"id":"deepseek-flash"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	run := func(args ...string) (string, error) {
		return stdoutOf(t, func() error { return providerCmd(append([]string{"provider", "models", "og"}, args...)) })
	}
	picks := func() []string { return mustFind(t, "og").Models }

	out, err := run("+deepseek-flash")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"m1", "m2", "m3", "deepseek-flash"}; !slices.Equal(picks(), want) {
		t.Fatalf("+deepseek-flash left %v; want %v", picks(), want)
	}
	if !strings.Contains(out, "3 → 4") || !strings.Contains(out, "deepseek-flash") {
		t.Errorf("+ said %q; want 3 → 4 and the model added", out)
	}
	// its provider/model id is taken too, and -m takes one out
	if _, err := run("-og/m2", "+m2"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("-m2"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"m1", "m3", "deepseek-flash"}; !slices.Equal(picks(), want) {
		t.Fatalf("-m2 left %v; want %v", picks(), want)
	}
	// taking out what isn't exposed, mixing the forms, or leaving none
	// is an error that changes nothing
	for _, args := range [][]string{{"-nope"}, {"m1", "+m2"}, {"-m1", "-m3", "-deepseek-flash"}} {
		if _, err := run(args...); err == nil {
			t.Errorf("%v was taken", args)
		}
		if want := []string{"m1", "m3", "deepseek-flash"}; !slices.Equal(picks(), want) {
			t.Fatalf("%v changed the picks to %v", args, picks())
		}
	}
	// the whole list replaces, as scripts already use it, and says so
	out, err = run("m1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(picks(), []string{"m1"}) {
		t.Fatalf("replace left %v", picks())
	}
	if !strings.Contains(out, "replaced: 3 → 1") || !strings.Contains(out, "+<model>") {
		t.Errorf("replace said %q; want 3 → 1 and how to add one instead", out)
	}
	// all gives the default back
	if _, err := run("all"); err != nil || picks() != nil {
		t.Fatalf("all left %v, %v", picks(), err)
	}
	// none serves none of them, kept for its balance (#1818); all gives
	// the vendor's list back, a pick just that one
	if _, err := run("none"); err != nil {
		t.Fatal(err)
	}
	if p := mustFind(t, "og"); !p.ServesNone() || len(p.Exposed()) != 0 {
		t.Fatalf("none left %v noModels %v, exposing %d", p.Models, p.NoModels, len(p.Exposed()))
	}
	if _, err := run("all"); err != nil {
		t.Fatal(err)
	}
	if p := mustFind(t, "og"); p.NoModels || len(p.Exposed()) != 4 {
		t.Fatalf("all after none: noModels %v, exposing %d", p.NoModels, len(p.Exposed()))
	}
	if _, err := run("none"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("m3"); err != nil {
		t.Fatal(err)
	}
	if p := mustFind(t, "og"); p.NoModels || !slices.Equal(p.Models, []string{"m3"}) {
		t.Fatalf("a pick after none: %v noModels %v", p.Models, p.NoModels)
	}
}

// uni-switch models and uni-switch provider <id> name the models a provider's list
// has that it doesn't expose, and why, so a group naming one ("no member
// ready (not served)") can be put right (MOMO on Discord: 35 exposed of 37,
// opencode-go/deepseek-flash not in uni-switch models).
func TestModelsSayWhatIsNotExposed(t *testing.T) {
	groupsHome(t)
	catalog.Changed = nil
	if err := provider.Save(provider.Provider{ID: "og", Name: "OG", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(catalog.LivePath("og")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.LivePath("og"), []byte(`{"models":[{"id":"m1"},{"id":"m2"},{"id":"deepseek-flash"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for name, f := range map[string]func() error{
		"models":   func() error { return models(nil) },
		"provider": func() error { return providerCmd([]string{"provider", "og"}) },
	} {
		out, err := stdoutOf(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "deepseek-flash") || !strings.Contains(out, "not picked") || !strings.Contains(out, "uni-switch provider models og +<model>") {
			t.Errorf("%s said %q; want deepseek-flash named as not picked, and how to expose it", name, out)
		}
	}
	// with nothing left out, nothing is said
	if err := provider.Save(provider.Provider{ID: "og", Name: "OG", Key: "k", Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if out, _ := stdoutOf(t, func() error { return models(nil) }); strings.Contains(out, "not exposed") {
		t.Errorf("with every model exposed, models said %q", out)
	}
}

// uni-switch provider models <id> best serves the newest of each family the
// vendor lists (Best in family, Steven on Discord); all gives the whole
// list back, and a pick or none clears it.
func TestProviderModelsBest(t *testing.T) {
	groupsHome(t)
	catalog.Changed = nil
	if err := provider.Save(provider.Provider{ID: "og", Name: "OG", Key: "k", Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(catalog.LivePath("og")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.LivePath("og"), []byte(`{"models":[{"id":"gpt-6.1-sol"},{"id":"gpt-6-astra"},{"id":"gpt-6-sol"},{"id":"gpt-6-luna"},{"id":"gpt-5.6-sol"},{"id":"gpt-5.6-terra"},{"id":"gpt-5.6-luna"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	run := func(args ...string) (string, error) {
		return stdoutOf(t, func() error { return providerCmd(append([]string{"provider", "models", "og"}, args...)) })
	}
	exposed := func() []string {
		var out []string
		for _, m := range mustFind(t, "og").Exposed() {
			out = append(out, m.ID)
		}
		return out
	}
	if _, err := run("best"); err != nil {
		t.Fatal(err)
	}
	if p := mustFind(t, "og"); !p.BestInFamily || p.Models != nil || p.NoModels {
		t.Fatalf("best saved %v noModels %v best %v", p.Models, p.NoModels, p.BestInFamily)
	}
	if want := []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna", "gpt-5.6-terra"}; !slices.Equal(exposed(), want) {
		t.Fatalf("best exposes %v, want %v", exposed(), want)
	}
	// the older ones are named as left out, and why
	out, err := stdoutOf(t, func() error { return providerCmd([]string{"provider", "og"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gpt-5.6-sol") || !strings.Contains(out, "best in family") {
		t.Errorf("provider og said %q; want gpt-5.6-sol left out by best in family", out)
	}
	if _, err := run("all"); err != nil {
		t.Fatal(err)
	}
	if p := mustFind(t, "og"); p.BestInFamily || len(p.Exposed()) != 7 {
		t.Fatalf("all after best: best %v exposing %d", p.BestInFamily, len(p.Exposed()))
	}
	for _, args := range [][]string{{"gpt-6-sol"}, {"none"}} {
		if _, err := run("best"); err != nil {
			t.Fatal(err)
		}
		if _, err := run(args...); err != nil {
			t.Fatal(err)
		}
		if p := mustFind(t, "og"); p.BestInFamily {
			t.Errorf("%v after best kept it", args)
		}
	}
}
