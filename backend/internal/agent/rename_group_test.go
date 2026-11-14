package agent

import (
	"path/filepath"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A change to the list alone never restarts the Codex app's app-server:
// a provider renamed rewrites the catalog, and the app shows the new name
// at its next launch (TestCodexEffortPickKeepsTheAppRunning says why).
func TestCodexAppRouteIgnoresARename(t *testing.T) {
	home := syncHome(t)
	codexCat := filepath.Join(home, ".codex", "uni-switch-models.json")
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"relay/glm-4.6\"\nmodel_provider = \"uni-switch\"\nmodel_catalog_json = '"+codexCat+"'\n")
	writeFile(t, codexCat, `{"models":[]}`)
	SyncCatalog()
	before, _, known := codexAppRoute()
	if !known {
		t.Fatal("route not known")
	}
	if !strings.Contains(readFile(codexCat), "GLM-4.6 · Relay") {
		t.Fatalf("catalog:\n%s", readFile(codexCat))
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Renamed", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6"}}); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	if !strings.Contains(readFile(codexCat), "GLM-4.6 · Renamed") {
		t.Fatalf("catalog not rewritten:\n%s", readFile(codexCat))
	}
	if r, _, _ := codexAppRoute(); r != before {
		t.Fatal("a provider renamed changed the Codex app's route: its app-server is ended for a list")
	}
}
