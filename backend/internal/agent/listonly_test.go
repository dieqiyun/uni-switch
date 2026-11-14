package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// yyddbds on Discord: in Xiaomi MiMo they added a custom provider pointing
// at uni-switch and picked a few of its models by hand. MiMo wasn't connected
// on uni-switch's Agents page, yet after an update the provider had every one
// of uni-switch's models: the start's catalog sync wrote uni-switch's whole list
// over any block named uni-switch. uni-switch's default never goes over a list the
// user wrote; one uni-switch wrote still follows the catalog.
func TestCatalogSyncLeavesAHandWrittenUniSwitchBlock(t *testing.T) {
	home := syncHome(t)
	if err := provider.Save(provider.Provider{ID: "added", Name: "Added", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m2"}}); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config")
	mimo := filepath.Join(cfgDir, "mimocode", "mimocode.json")
	// as the app writes a custom provider: uni-switch's address, a key, the
	// models the user picked; the model they run is MiMo's own
	own := `{
  "model": "mimo/mimo-v2.5-pro",
  "provider": {
    "uni-switch": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "uni-switch",
      "options": {"baseURL": "http://127.0.0.1:3425/v1", "apiKey": "sk-mine"},
      "models": {"relay/glm-4.6": {"name": "GLM 4.6"}}
    }
  }
}
`
	writeFile(t, mimo, own)
	// a sibling on the same sync: Pi with a uni-switch provider written by
	// hand in models.json, its default its own
	piModels := filepath.Join(home, ".pi", "agent", "models.json")
	piOwn := `{"providers":{"uni-switch":{"baseUrl":"http://127.0.0.1:3425/v1","api":"openai-completions","models":[{"id":"relay/glm-4.6"}]}}}`
	writeFile(t, piModels, piOwn)
	writeFile(t, filepath.Join(home, ".pi", "agent", "settings.json"), `{"defaultProvider":"anthropic","defaultModel":"claude-x"}`)
	// and Crush
	crushCfg := filepath.Join(cfgDir, "crush", "crush.json")
	crushOwn := `{"providers":{"uni-switch":{"type":"openai-compat","base_url":"http://127.0.0.1:3425/v1","models":[{"id":"relay/glm-4.6"}]}}}`
	writeFile(t, crushCfg, crushOwn)

	SyncCatalog()
	if got := readFile(mimo); got != own {
		t.Errorf("MiMo's own uni-switch provider written over:\n%s", got)
	}
	if got := readFile(piModels); got != piOwn {
		t.Errorf("Pi's own uni-switch provider written over:\n%s", got)
	}
	if got := readFile(crushCfg); got != crushOwn {
		t.Errorf("Crush's own uni-switch provider written over:\n%s", got)
	}

	// connected through uni-switch, the list follows the catalog
	a := mimocode(home, cfgDir)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "later", Name: "Later", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m3"}}); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	if got := readFile(mimo); !strings.Contains(got, `"later/m3"`) || !strings.Contains(got, `"added/m2"`) {
		t.Errorf("a connected MiMo's list not kept current:\n%s", got)
	}

	// switched off on the Agents page, then a provider of that name
	// written by hand again: it is the user's
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, mimo, own)
	SyncCatalog()
	if got := readFile(mimo); got != own {
		t.Errorf("after Disconnect, MiMo's own uni-switch provider written over:\n%s", got)
	}

	// a model of uni-switch's picked in MiMo on that provider of its own
	// doesn't connect it (JFunfunfun on X: handWired); connecting it in
	// uni-switch does, and the list is uni-switch's again
	picked := strings.Replace(own, `"mimo/mimo-v2.5-pro"`, `"uni-switch/relay/glm-4.6"`, 1)
	writeFile(t, mimo, picked)
	SyncCatalog()
	if got := readFile(mimo); got != picked {
		t.Errorf("a uni-switch model picked on MiMo's own uni-switch provider, written over:\n%s", got)
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	if got := readFile(mimo); !strings.Contains(got, `"later/m3"`) {
		t.Errorf("a connected MiMo's list not kept current:\n%s", got)
	}
}

// A catalog sync runs a ListOnly agent's Sync only while it reads as
// connected: every one of them connected by uni-switch reads so, or its list
// would never follow the catalog again.
func TestListOnlyAgentsReadConnected(t *testing.T) {
	codexHome(t, "", "")
	n := 0
	for _, a := range All() {
		if !a.ListOnly || a.WSL != "" {
			continue
		}
		n++
		if err := a.Connect(); err != nil {
			t.Errorf("%s: %v", a.ID, err)
			continue
		}
		if !a.Wired() {
			t.Errorf("%s connected but not Wired: a catalog sync would skip its list", a.ID)
		}
	}
	if n < 15 {
		t.Fatalf("only %d ListOnly agents", n)
	}
}
