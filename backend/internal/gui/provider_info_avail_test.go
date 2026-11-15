package gui

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider's row on the Providers page reads the vendor's list once:
// it was read four times (the picks, what is exposed twice, the newest
// of each family), each built whole with its names and levels, for each
// provider on every poll. What the row says is the same.
func TestProviderInfoReadsTheListOnce(t *testing.T) {
	h := sandboxHome(t)
	var b strings.Builder
	b.WriteString(`{"relayco": {"models": {`)
	for i := range 2000 {
		fmt.Fprintf(&b, `"m-%d": {"id":"m-%d","name":"M %d"},`, i, i, i)
	}
	b.WriteString(`"x": {"id":"x"}}}}`)
	os.MkdirAll(h+"/.cache/uni-switch", 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	p := provider.Provider{ID: "r", Name: "R", Key: "k", Chat: "http://127.0.0.1:1/v1", Catalog: "relayco", Models: []string{"m-3", "m-1", "gone"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(provider.Path(), old, old)
	os.Chtimes(catalog.CachePath(), old, old)
	if len(p.Available()) < 2000 {
		t.Fatalf("the catalog's models not listed: %d", len(p.Available()))
	}

	// as a request to the page reads it (held)
	defer provider.Hold()()
	defer filememo.Hold()()
	got := providerInfo(p, nil)
	if !slices.Equal(got.Chosen, p.Picks()) {
		t.Fatalf("chosen %v, picks %v", got.Chosen, p.Picks())
	}
	if got.Exposed != len(p.Exposed()) || got.Models[0].ID != "gone" {
		t.Fatalf("exposed %d of %d, first %+v", got.Exposed, len(p.Exposed()), got.Models[0])
	}

	if raceOn {
		t.Skip("-race: sync.Pool drops what it holds, so allocations are not what a build allocates")
	}
	bytesPer := func(f func()) uint64 {
		f()
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		for range 5 {
			f()
		}
		runtime.ReadMemStats(&b)
		return (b.TotalAlloc - a.TotalAlloc) / 5
	}
	list := bytesPer(func() { p.Available() })
	row := bytesPer(func() { providerInfo(p, nil) })
	t.Logf("a row %d bytes, the list %d", row, list)
	// each model's own look-ups besides (its price, window, protocols)
	// are about 2.5 lists here; reading it four times over made 6.8
	if row > list*5 {
		t.Fatalf("a row took %d bytes, the list %d: read more than once", row, list)
	}
}
