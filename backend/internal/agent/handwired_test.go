package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// JFunfunfun on X: Pi set by hand onto a provider of its own named uni-switch
// (uni-switch/<model>, at uni-switch's gateway) read as connected in uni-switch, which
// then wrote its own list over that provider's models on every catalog
// change, and Disconnect took the provider out. A provider named uni-switch
// that uni-switch didn't put there is the user's: the agent isn't connected,
// nothing drifts, and neither a sync, a rename nor Disconnect touches its
// files. Connecting it in uni-switch takes it on.
func TestPiHandWiredUniSwitchProviderIsTheirs(t *testing.T) {
	syncHome(t)
	a, err := Find("pi")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(a.Path)
	models := filepath.Join(dir, "models.json")
	// Pi's own layout, as a person writes it (pi.dev docs: models.json
	// providers, settings.json defaultProvider/defaultModel)
	settingsJSON := `{
  "defaultProvider": "uni-switch",
  "defaultModel": "relay/glm-4.6",
  "defaultThinkingLevel": "medium"
}
`
	modelsJSON := `{
  "providers": {
    "uni-switch": {
      "baseUrl": "` + gatewayV1() + `",
      "api": "openai-completions",
      "apiKey": "sk-a-gateway-key-of-mine",
      "models": [{ "id": "relay/glm-4.6", "name": "my GLM" }]
    }
  }
}
`
	hanakoWrite(t, a.Path, settingsJSON, 0o644)
	hanakoWrite(t, models, modelsJSON, 0o644)
	same := func(when string) {
		t.Helper()
		for p, want := range map[string]string{a.Path: settingsJSON, models: modelsJSON} {
			if b, _ := os.ReadFile(p); string(b) != want {
				t.Errorf("%s rewrote %s:\n%s", when, filepath.Base(p), b)
			}
		}
	}
	if v := a.Values()["model"]; v != "uni-switch/relay/glm-4.6" {
		t.Fatalf("model reads %q", v)
	}
	if a.Wired() {
		t.Fatal("a provider named uni-switch written by hand reads as connected")
	}
	if d := a.Drift(); d != nil {
		t.Errorf("drift on a config uni-switch never set: %+v", d)
	}
	// a provider saved: the catalog changed, every agent's list is synced
	if err := provider.Save(provider.Provider{ID: "relay2", Name: "Relay 2", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6"}}); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	same("SyncCatalog")
	if _, err := RenameProvider("relay", "relay3"); err != nil {
		t.Fatal(err)
	}
	same("RenameProvider")
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	same("Disconnect")
	if a.Wired() {
		t.Fatal("connected after Disconnect")
	}

	// connected in uni-switch, it is uni-switch's: Keep (taking the config as it
	// is) leaves it so, Disconnect gives it up
	hanakoWrite(t, a.Path, `{}`, 0o644)
	os.Remove(models)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if !a.Wired() {
		t.Fatalf("not connected after Connect: %v", a.Values())
	}
	a.Keep()
	if !a.Wired() {
		t.Fatal("Keep took uni-switch's connection away")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Wired() {
		t.Fatalf("connected after Disconnect: %v", a.Values())
	}
}

// Users who connected before this release read as connected still: an
// older uni-switch recorded the model it set (applied.json), or, where Keep
// took that record away, stashed what the agent was on at Connect.
func TestPiConnectedBeforeStaysConnected(t *testing.T) {
	for _, c := range []struct {
		name string
		mark func(a *Agent)
	}{
		{"applied", func(a *Agent) { record(a.ID, "model", "uni-switch/relay/glm-4.6") }},
		{"applied, model since gone", func(a *Agent) { record(a.ID, "model", "uni-switch/gone/model-x") }},
		{"kept before", func(a *Agent) { stash(map[string]string{a.ID + ".connect.was": `{"model":""}`}) }},
		{"applied, then kept", func(a *Agent) { record(a.ID, "model", "uni-switch/relay/glm-4.6"); a.Keep() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			syncHome(t)
			a, err := Find("pi")
			if err != nil {
				t.Fatal(err)
			}
			// as uni-switch wrote it: the model field's Set, with nothing recorded
			if err := a.Field("model").Set("uni-switch/relay/glm-4.6"); err != nil {
				t.Fatal(err)
			}
			if a.Wired() {
				t.Fatal("unrecorded reads as connected")
			}
			c.mark(a)
			if !a.Wired() {
				t.Fatalf("connected before, reads as not connected: %v", a.Values())
			}
		})
	}
}

// Every agent whose uni-switch is a provider of its own config (Spelled) holds
// a provider uni-switch didn't put there as the user's, as Pi does.
func TestHandWiredUniSwitchProviderSiblings(t *testing.T) {
	ids := []string{"agy", "opencode", "openchamber", "mimocode", "deveco", "pi", "aside", "omo", "prime-agent", "goose", "zed", "vscode",
		"crush", "dsh", "ds-bot", "commandcode", "fx", "omp", "hermes", "morph", "kimi", "empryo", "minimax-code", "droid",
		"qoder", "qoder-cn", "grok", "atomcode", "cline", "snow"}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			if id == "aside" {
				// connected natively, Aside's provider named uni-switch is
				// uni-switch's by the key uni-switch wrote in it; one with a key of
				// the user's is theirs (asideConnection.provider)
				settings, models := asideHome(t)
				writeFile(t, settings, `{"defaultModel":{"provider":"uni-switch","modelId":"relay/glm-4.6"}}`)
				writeFile(t, models, `{"providers":{"uni-switch":{"apiKey":"uni-switch","baseUrl":"`+gatewayV1()+`","api":"openai-completions","models":[{"id":"relay/glm-4.6"}]}}}`)
				a, err := Find(id)
				if err != nil {
					t.Fatal(err)
				}
				if a.Wired() {
					t.Fatal("Aside's provider named uni-switch, with the user's key, reads as connected")
				}
				return
			}
			syncHome(t)
			a, err := Find(id)
			if err != nil {
				t.Fatal(err)
			}
			if a.Spelled == nil {
				t.Fatal("no Spelled")
			}
			if id == "cline" {
				hanakoWrite(t, a.Path, `{"version":1,"modes":{},"providers":{},"lastUsedProvider":"openrouter"}`, 0o600)
			}
			// written outside uni-switch: the field's own Set, nothing recorded
			if err := a.Fields[0].Set("uni-switch/relay/glm-4.6"); err != nil {
				t.Fatal(err)
			}
			if a.Wired() {
				t.Fatalf("a provider named uni-switch uni-switch didn't set reads as connected: %v", a.Values())
			}
			if d := a.Drift(); d != nil {
				t.Errorf("drift: %+v", d)
			}
			if err := a.Connect(); err != nil {
				t.Fatal(err)
			}
			if !a.Wired() {
				t.Fatalf("not connected after Connect: %v", a.Values())
			}
			if err := a.Disconnect(); err != nil {
				t.Fatal(err)
			}
			if a.Wired() {
				t.Fatalf("still connected after Disconnect: %v", a.Values())
			}
		})
	}
}

// setUpInuni-switch marks agents as connected in uni-switch, as Connect leaves them
// (its connect.was stash), for a test that writes a connected agent's files
// itself.
func setUpInUniSwitch(ids ...string) {
	for _, id := range ids {
		stash(map[string]string{id + ".connect.was": "{}"})
	}
}
