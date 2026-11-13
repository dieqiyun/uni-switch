package gateway

import (
	"fmt"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Whether a model an agent asks for is served here is answered from the
// catalog's names kept by provider.StampNow: every request an agent sends
// asks it (standIn), and each one built the whole catalog to look, 2.26 GB
// of 9.8 GB in 10 minutes of ~1,000 requests. A provider saved since is
// seen at once.
func TestUnservedKeptWhileCatalogStays(t *testing.T) {
	setup(t, provider.Chat, &fake{})
	models := make([]string, 1000)
	for i := range models {
		models[i] = fmt.Sprintf("m%d", i)
	}
	if err := provider.Save(provider.Provider{ID: "big", Name: "Big", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	if unserved("m1") || unserved("big/m150") || !unserved("claude-sonnet-4-6") || !unserved("nope/claude-sonnet-4-6") {
		t.Fatal("served wrong before anything is kept")
	}
	// ids with a provider in them, as uni-switch writes into agents' configs:
	// a bare one asks the routing groups first, which the request's
	// routing builds too
	allocs := testing.AllocsPerRun(20, func() {
		unserved("big/m150")
		unserved("nope/claude-sonnet-4-6")
	})
	// measured against one catalog build in the same run, since the
	// package's other tests leave background work allocating beside it:
	// two answers built it twice (~2x), kept they are StampNow's look at
	// three files and Find's of the providers for the id no entry has
	built := testing.AllocsPerRun(5, func() { provider.Catalog() })
	if allocs*3 > built {
		t.Fatalf("%.0f allocations for two answers already given; one catalog build is %.0f", allocs, built)
	}

	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-sonnet-4-6"}}); err != nil {
		t.Fatal(err)
	}
	if unserved("claude-sonnet-4-6") {
		t.Fatal("a model of a provider saved since is still unserved")
	}
}
