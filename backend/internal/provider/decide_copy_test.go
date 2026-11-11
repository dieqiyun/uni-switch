package provider

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Each of a provider's models is asked whether it decides, and each ask
// copied the provider's whole decision list (0.4 GB of a real config's
// 4 GB over 20 polls of the Providers and Agents pages). An ask reads the
// list kept, and answers as the list does.
func TestDecidesModelCopiesNoList(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := Provider{ID: "or-many", Name: "OpenRouter", Key: "k", Chat: "https://openrouter.ai/api/v1", Decide: "https://openrouter.ai/api/v1"}
	if !p.listsDecisions() {
		t.Fatal("not a provider that lists its decision models")
	}
	// none listed yet: OpenRouter's Jev
	if !p.DecidesModel(OpenRouterJev) || p.DecidesModel("liquid/d1") {
		t.Fatal("with no list, only Jev decides")
	}
	var ms []catalog.Model
	for i := range 2000 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("vendor/decider-%d", i)})
	}
	if err := catalog.SaveLive(decisionsID(p.ID), p.Decide, ms); err != nil {
		t.Fatal(err)
	}
	fetchedEarlier(t, catalog.LivePath(decisionsID(p.ID)))
	if !p.DecidesModel("vendor/decider-1999") || p.DecidesModel(OpenRouterJev) || p.DecidesModel("vendor/chat") {
		t.Fatal("decision models are not the list's")
	}
	listed := bytesPer(func() { p.decideListed() })
	if n := bytesPer(func() { p.DecidesModel("vendor/decider-1999") }); n*20 > listed {
		t.Fatalf("an ask took %d bytes, the list %d", n, listed)
	}

}

// bytesPer is what one call of f allocates, in bytes, over 20 calls.
func bytesPer(f func()) uint64 {
	f()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	for range 20 {
		f()
	}
	runtime.ReadMemStats(&b)
	return (b.TotalAlloc - a.TotalAlloc) / 20
}

// fetchedEarlier dates f an hour back, as a list fetched earlier is: a file
// written just now is read again on each look, its time too recent to
// tell a later write apart.
func fetchedEarlier(t *testing.T, f string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
}
