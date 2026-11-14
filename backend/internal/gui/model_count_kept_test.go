package gui

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// An agent's model count is kept while nothing it is counted from changed:
// every poll of the Agents page counted each agent's list anew, its whole
// ListedFor built for a number. A provider or a model hidden since is
// counted at once.
func TestModelCountKeptWhileNothingChanged(t *testing.T) {
	sandboxHome(t)
	var models []string
	for i := range 300 {
		models = append(models, fmt.Sprintf("m-%d", i))
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	// written long enough ago that its time tells a later write apart
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(provider.Path(), old, old); err != nil {
		t.Fatal(err)
	}
	c := modelCount("codex")
	if c.Listed < 300 || c.Shown != c.Listed {
		t.Fatalf("relay's models not counted: %+v", c)
	}
	bytesPer := func(f func()) uint64 {
		f()
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		for range 20 {
			f()
		}
		runtime.ReadMemStats(&b)
		return (b.TotalAlloc - a.TotalAlloc) / 20
	}
	built := bytesPer(func() { countModels("codex") })
	kept := bytesPer(func() { modelCount("codex") })
	if kept*20 > built {
		t.Fatalf("a count already made took %d bytes, counting %d", kept, built)
	}

	// each caller's copy is its own
	mine := modelCount("codex")
	mine.Names[0] = "mine"
	mine.By[0].N = -1
	if c := modelCount("codex"); c.Names[0] == "mine" || c.By[0].N < 0 {
		t.Fatalf("a caller's write reached the count kept: %+v", c)
	}

	if err := provider.Save(provider.Provider{ID: "nb", Name: "NB", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if got := modelCount("codex"); got.Listed != c.Listed+1 {
		t.Fatalf("a provider saved since is not counted: %d, was %d", got.Listed, c.Listed)
	}
	s := settings.Load()
	s.HiddenModels = map[string][]string{"codex": {"relay/m-1"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if got := modelCount("codex"); got.Shown != got.Listed-1 || slices.Contains(got.Names, "m-1") {
		t.Fatalf("a model hidden since is still counted: %+v", got)
	}
}
