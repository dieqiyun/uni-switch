package gui

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Picks the vendor's list doesn't know are shown first, the last one
// picked foremost, then the listed models in the list's order.
func TestUnlistedPicksShownFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := provider.Provider{ID: "relay", Name: "Relay", Chat: "https://relay.example/v1"}
	ms := func(ids ...string) (out []catalog.Model) {
		for _, id := range ids {
			out = append(out, catalog.Model{ID: id})
		}
		return
	}
	info := providerInfoWith(p, provider.Listing{Available: ms("a", "b"), Exposed: ms("a", "x", "y", "z")}, nil, nil)
	var ids []string
	for _, m := range info.Models {
		ids = append(ids, m.ID)
	}
	if want := []string{"z", "y", "x", "a", "b"}; !slices.Equal(ids, want) {
		t.Fatalf("models shown as %v, want %v", ids, want)
	}
	if info.Exposed != 4 {
		t.Fatalf("%d exposed, want 4", info.Exposed)
	}
}

// A row's models and agents are each made once, at their size: grown by
// append for the picks the list lacks and again to put them in front, it
// was 8 MB of what twenty polls of the Providers page allocated with real
// accounts.
func TestProviderRowListsMadeAtTheirSize(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := provider.Provider{ID: "relay", Name: "Relay", Chat: "https://relay.example/v1"}
	var avail, exposed []catalog.Model
	for i := range 100 {
		avail = append(avail, catalog.Model{ID: fmt.Sprintf("m-%d", i)})
	}
	for i := range 7 {
		exposed = append(exposed, catalog.Model{ID: fmt.Sprintf("pick-%d", i)})
	}
	exposed = append(exposed, avail[3])
	agents := []agentUse{{Agent: &agent.Agent{ID: "a"}}, {Agent: &agent.Agent{ID: "b"}}, {Agent: &agent.Agent{ID: "c"}}}
	info := providerInfoWith(p, provider.Listing{Available: avail, Exposed: exposed}, agents, nil)
	if len(info.Models) != 107 || info.Models[0].ID != "pick-6" || info.Models[7].ID != "m-0" {
		t.Fatalf("%d models, first %q, eighth %q", len(info.Models), info.Models[0].ID, info.Models[7].ID)
	}
	if cap(info.Models) != len(info.Models) {
		t.Fatalf("models made for %d, %d rows: grown on the way", cap(info.Models), len(info.Models))
	}
	if len(info.Agents) != 3 || cap(info.Agents) != 3 {
		t.Fatalf("agents made for %d, %d rows", cap(info.Agents), len(info.Agents))
	}
}
