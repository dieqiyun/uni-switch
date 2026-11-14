package provider

import (
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

// Catalog's list is made once with room for every entry: cloned at the
// groups' size and grown by append for the providers' models, it was 29
// MB of what forty polls of the Providers page allocated with real
// accounts, about twice the list it gave.
func TestCatalogMadeOnce(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	defer func(d, k time.Duration) { heldFor, keptFor = d, k }(heldFor, keptFor)
	heldFor, keptFor = time.Minute, time.Minute
	var models []string
	for i := range 400 {
		models = append(models, "m"+strconv.Itoa(i))
	}
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	defer Hold()()
	cat := Catalog() // the entries and groups, built and held
	if len(cat) < 400 || !slices.ContainsFunc(cat, func(e Entry) bool { return e.ID == "relay/m399" }) {
		t.Fatalf("the catalog: %d entries", len(cat))
	}
	list := uint64(len(cat)) * uint64(unsafe.Sizeof(Entry{}))
	var all uint64
	const runs = 10
	for range runs {
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		Catalog()
		runtime.ReadMemStats(&m1)
		all += m1.TotalAlloc - m0.TotalAlloc
	}
	if per := all / runs; per > list*3/2 {
		t.Fatalf("a catalog of %d bytes allocated %d to make", list, per)
	}
}
