package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Requests further apart than heldFor resolve their models from the
// catalog the first one built while nothing it is built from moved
// (StampNow): each built it again from every provider's models and
// accounts, ~725 MB of allocations in 300s of a gateway in use. A
// provider saved, or keptFor gone by, has it built again, and the next
// request sees what was saved. The groups built from it are kept with it.
func TestCatalogKeptAcrossRequests(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	defer func(d, k time.Duration) { heldFor, keptFor = d, k }(heldFor, keptFor)
	heldFor = 0           // each request after the last one's hold has gone
	keptFor = time.Minute // a loaded machine's pause isn't the bound
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	// as a request to the gateway: the model resolved, and the served
	// list (groups and all) read for its reply limit
	request := func(id string) bool {
		defer Hold()()
		_, _, ok := Resolve(id)
		Served()
		return ok
	}
	if !request("relay/m1") {
		t.Fatal("relay/m1 not resolved")
	}
	before := entriesBuilt.Load()
	for range 20 {
		if !request("relay/m2") {
			t.Fatal("relay/m2 not resolved")
		}
	}
	if n := entriesBuilt.Load() - before; n != 0 {
		t.Fatalf("20 requests with nothing changed built the catalog %d times, want 0", n)
	}

	// a provider saved: the next request sees it
	if err := Save(Provider{ID: "relay2", Name: "Relay 2", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m3"}}); err != nil {
		t.Fatal(err)
	}
	if !request("relay2/m3") {
		t.Fatal("a provider saved isn't resolved by the next request")
	}
	if e := entriesOfID(t, "relay2/m3"); !e {
		t.Fatal("relay2/m3 not in the served catalog after the save")
	}

	// providers.json written by another process (the CLI): seen at once
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	before = entriesBuilt.Load()
	if err := os.WriteFile(Path(), append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	request("relay/m1")
	if n := entriesBuilt.Load() - before; n != 1 {
		t.Fatalf("after another process wrote providers.json: built %d times, want 1", n)
	}

	// keptFor gone by: built again, for what moves no stamp
	keptFor = 0
	before = entriesBuilt.Load()
	request("relay/m1")
	request("relay/m1")
	if n := entriesBuilt.Load() - before; n < 2 {
		t.Fatalf("past keptFor: built %d times in 2 requests, want one each at least", n)
	}

}

func entriesOfID(t *testing.T, id string) bool {
	t.Helper()
	defer Hold()()
	for _, e := range Served() {
		if e.ID == id {
			return true
		}
	}
	return false
}
