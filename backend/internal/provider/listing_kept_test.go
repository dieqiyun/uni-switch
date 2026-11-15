package provider

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The Providers page's polls read each provider's models from a Listing
// kept with the catalog while nothing it is built from moved, not built
// again on each poll; a provider saved has it built again, and the next
// poll draws what was saved.
func TestProvidersListingKeptAcrossPolls(t *testing.T) {
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
	poll := func() []string {
		defer Hold()()
		var ids []string
		for _, p := range All() {
			if p.ID != "relay" {
				continue
			}
			l := KeptListing(p)
			for _, m := range l.Exposed {
				ids = append(ids, m.ID)
			}
			if fresh := p.Listing(); len(fresh.Exposed) != len(l.Exposed) || len(fresh.Available) != len(l.Available) || len(fresh.Newest) != len(l.Newest) {
				t.Fatalf("kept %d/%d/%d models, built afresh %d/%d/%d", len(l.Available), len(l.Exposed), len(l.Newest), len(fresh.Available), len(fresh.Exposed), len(fresh.Newest))
			}
		}
		return ids
	}
	if got := poll(); !slices.Equal(got, []string{"m1", "m2"}) {
		t.Fatalf("exposed %v", got)
	}
	before := listingsBuilt.Load()
	for range 20 {
		poll()
	}
	if n := listingsBuilt.Load() - before; n != 0 {
		t.Fatalf("20 polls with nothing changed built the listing %d times, want 0", n)
	}
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m3"}}); err != nil {
		t.Fatal(err)
	}
	if got := poll(); !slices.Equal(got, []string{"m3"}) {
		t.Fatalf("after a save the poll draws %v, want [m3]", got)
	}
}
