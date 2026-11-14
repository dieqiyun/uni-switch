package provider

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// An agent's catalog is kept with the catalog across a window's polls
// while nothing it is built from moved, not built again from every
// provider's models on each; a provider saved, or a model taken out of
// the agent's list, has it built again. A caller's change to its lists
// never reaches the next caller's.
func TestCatalogForKeptAcrossPolls(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	defer func(d, k time.Duration) { heldFor, keptFor = d, k }(heldFor, keptFor)
	heldFor = 0           // each poll after the last one's hold has gone
	keptFor = time.Minute // a loaded machine's pause isn't the bound
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	ids := func(es []Entry) []string {
		var out []string
		for _, e := range es {
			if e.Provider.ID == "relay" {
				out = append(out, e.ID)
			}
		}
		return out
	}
	every := func(es []Entry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.ID)
		}
		return out
	}
	poll := func() (shown, hidden []string) {
		defer Hold()()
		s, h := CatalogFor("opencode")
		fs, fh := catalogFor("opencode")
		if !slices.Equal(every(s), every(fs)) || !slices.Equal(every(h), every(fh)) {
			t.Fatalf("kept %v/%v, built afresh %v/%v", every(s), every(h), every(fs), every(fh))
		}
		shown, hidden = ids(s), ids(h)
		if len(s) > 0 {
			s[0].ID = "written" // the caller's own
		}
		return shown, hidden
	}
	if s, _ := poll(); !slices.Equal(s, []string{"relay/m1", "relay/m2"}) {
		t.Fatalf("shown %v", s)
	}
	before := catalogsBuilt.Load()
	for range 20 {
		poll()
	}
	if n := catalogsBuilt.Load() - before; n != 0 {
		t.Fatalf("20 polls with nothing changed built the catalog %d times, want 0", n)
	}
	if err := SetHiddenModels("opencode", []string{"relay/m1"}); err != nil {
		t.Fatal(err)
	}
	if s, h := poll(); !slices.Equal(s, []string{"relay/m2"}) || !slices.Equal(h, []string{"relay/m1"}) {
		t.Fatalf("after hiding m1: shown %v, hidden %v", s, h)
	}
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m3"}}); err != nil {
		t.Fatal(err)
	}
	if s, _ := poll(); !slices.Equal(s, []string{"relay/m3"}) {
		t.Fatalf("after a save the poll shows %v, want [relay/m3]", s)
	}
}
