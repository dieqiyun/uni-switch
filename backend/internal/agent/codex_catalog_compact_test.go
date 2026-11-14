package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Codex is told where to compact a long conversation (#876) both from the
// gateway's /models (provider.CodexListed, which sets catalog.Model.Compact)
// and from the list uni-switch writes for it on disk (uni-switch-models.json,
// codex.go:593 -> codexcat.Catalog(uniSwitchModels("codex"))).
//
// uniSwitchModels left Compact unset, so codexcat fell back to the one for
// every model: a threshold the user set on a model or on its provider
// reached the gateway's list but not the one Codex reads from disk.
//
// syncHome gives relay serving glm-4.6, whose models.dev window is 204800.
func TestCodexCatalogCompactAtReachesDisk(t *testing.T) {
	home := syncHome(t)
	// a threshold changed on a provider rewrites the agents' lists
	catalog.Changed = SyncCatalog
	t.Cleanup(func() { catalog.Changed = nil })
	if err := provider.SetModelCompacts("relay", map[string]int{"*": 100000}); err != nil {
		t.Fatalf("SetModelCompacts: %v", err)
	}
	// connect Codex to uni-switch on a uni-switch model, as the app does: it writes
	// the catalog it reads from disk
	cx := codex(home)
	if err := cx.Fields[0].Set("relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}

	contextOf := func() (window, at int) {
		t.Helper()
		var got struct {
			Models []struct {
				Slug          string `json:"slug"`
				ContextWindow int    `json:"context_window"`
				Compact       int    `json:"auto_compact_token_limit"`
			} `json:"models"`
		}
		raw := readFile(filepath.Join(home, ".codex", "uni-switch-models.json"))
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		for _, m := range got.Models {
			if m.Slug == "relay/glm-4.6" {
				return m.ContextWindow, m.Compact
			}
		}
		slugs := make([]string, 0, len(got.Models))
		for _, m := range got.Models {
			slugs = append(slugs, m.Slug)
		}
		t.Fatalf("relay/glm-4.6 is not in the on-disk catalog; slugs: %v", slugs)
		return 0, 0
	}

	// relay's own "*" threshold, below the model's window: Codex is told
	// the model's whole window, which it shows, and compacts at the
	// threshold
	if window, at := contextOf(); window != 204800 || at != 100000 {
		t.Fatalf("with relay's Compact at 100000: context_window = %d, auto_compact_token_limit = %d; want 204800 and 100000", window, at)
	}

	// the model's own threshold comes before its provider's
	if err := provider.SetModelCompacts("relay", map[string]int{"*": 100000, "glm-4.6": 150000}); err != nil {
		t.Fatal(err)
	}
	if window, at := contextOf(); window != 204800 || at != 150000 {
		t.Fatalf("with relay/glm-4.6's own Compact at 150000: context_window = %d, auto_compact_token_limit = %d; want 204800 and 150000", window, at)
	}

	// taken away, the one for every model answers again: the whole window,
	// as the default threshold sits above this model's
	if err := provider.SetModelCompacts("relay", nil); err != nil {
		t.Fatal(err)
	}
	if window, at := contextOf(); window != 204800 || at != 0 {
		t.Fatalf("with no threshold set: context_window = %d, auto_compact_token_limit = %d; want the window and no threshold", window, at)
	}
}
