package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The image models a Google sign-in can draw with come from the model list
// the caller has (the Providers page's), read once, not from a list read
// again for them.
func TestDrawersOfTheCallersList(t *testing.T) {
	p := provider.Provider{ID: "antigravity", Account: &provider.Account{Agent: "antigravity"}}
	calls := 0
	got := DrawersOf(p, func() []catalog.Model {
		calls++
		return []catalog.Model{{ID: "gemini-3-pro"}, {ID: agDrawer}}
	})
	if len(got) != 1 || got[0].ID != agDrawer || got[0].Provider != "antigravity" {
		t.Fatalf("drawers %+v, want %s alone", got, agDrawer)
	}
	if calls != 1 {
		t.Fatalf("the list was read %d times", calls)
	}
}
