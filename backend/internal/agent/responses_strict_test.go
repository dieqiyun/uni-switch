package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// dshModelOf is the model id of a route as dshPiProviders parsed it.
func dshModelOf(t *testing.T, ps map[string]dshPiRoute, key, id string) *dshPiModel {
	t.Helper()
	r, ok := ps[key]
	if !ok {
		t.Fatalf("no route %s", key)
	}
	for i := range r.Models {
		if r.Models[i].ID == id {
			return &r.Models[i]
		}
	}
	t.Fatalf("route %s has no %s: %+v", key, id, r.Models)
	return nil
}

// #3055 (ArakawaHenri): OpenAI's Responses API takes a function tool whose
// strict is left out as a strict-mode one and normalizes its schema, where
// every property is required, so a tool's optional parameters came back
// filled. pi-ai says strict: false on an ordinary tool only when the
// model's compat says supportsStrictMode, which it takes as false where
// the compat is left out — as uni-switch's routes were. uni-switch now says it on
// every model of a route whose api stands as openai-responses (the one a
// user picked in dsh's Models page kept), and on none of a Chat or
// Messages route.
func TestDshTellsAResponsesToolsStrictIsOff(t *testing.T) {
	home, dir, web := dshProtocolHome(t)
	dshKeyFixture(dir)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"), 0o644)
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/resp/gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	ps := dshPiProviders(t, web)
	if m := dshModelOf(t, ps, "uni-switch-responses", "resp/gpt-5.5"); m.Compat == nil || !m.Compat.SupportsStrictMode {
		t.Fatalf("a model asked on Responses isn't told strict is off: %+v", m)
	}
	if m := dshModelOf(t, ps, "uni-switch", "deepseek/pro"); m.Compat != nil && m.Compat.SupportsStrictMode {
		t.Fatalf("a Chat model was told strict is off: %+v", m)
	}
	if dshModelOf(t, ps, "uni-switch", "deepseek/flash").Compat != nil {
		t.Fatal("a Chat model took a compat of its own")
	}
	if m := dshModelOf(t, ps, "uni-switch-anthropic", "anth/claude-sonnet-5"); m.Compat == nil || !m.Compat.ForceAdaptiveThinking || m.Compat.SupportsStrictMode {
		t.Fatalf("a Claude on Messages: %+v", m)
	}

	// a round over what it wrote leaves it as it is
	was, _ := os.ReadFile(web)
	dshWiredOnce()
	if again, _ := os.ReadFile(web); string(again) != string(was) {
		t.Fatalf("written again:\n%s", again)
	}

	// the api a user picked for a route in dsh's Models page decides, not
	// the route's name: uni-switch's first route on Responses tells its Chat
	// models strict is off and keeps a key of theirs beside uni-switch's; the
	// responses route on Chat tells its model nothing there
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"+
		"- id: llm-pi-ai # uni-switch\n  name: \"@deepseek-ai/dsh-llm-pi-ai\"\n  config:\n    providers:\n"+
		"      uni-switch:\n        displayName: uni-switch\n        apiKeyEnv: "+dshKeyRef+"\n        api: openai-responses\n        baseURL: "+gatewayV1()+"\n        models:\n"+
		"          - id: deepseek/pro\n            compat:\n              maxTokensField: max_tokens\n"+
		"      uni-switch-responses:\n        displayName: uni-switch · Responses\n        apiKeyEnv: "+dshKeyRef+"\n        api: openai-completions\n        baseURL: "+gatewayV1()+"\n        models:\n"+
		"          - id: resp/gpt-5.5\n"), 0o644)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(web)
	_, compat := dshRouteCompat(t, web, "uni-switch")
	c := compat["deepseek/pro"]
	if c["supportsStrictMode"] != true || c["maxTokensField"] != "max_tokens" {
		t.Fatalf("a Chat model on a route switched to Responses: %v\n%s", c, b)
	}
	_, compat = dshRouteCompat(t, web, "uni-switch-responses")
	if c = compat["resp/gpt-5.5"]; c != nil && c["supportsStrictMode"] == true {
		t.Fatalf("a model on a route switched to Chat was told strict is off: %v\n%s", c, b)
	}
}

// The same for Pi's and StepCode's model entries (piModelJSON): a model
// best served on Responses carries compat.supportsStrictMode, a Chat
// model's entry carries no compat at all. Pencil's entries stay on Chat
// throughout, so it takes none.
func TestPiTellsAResponsesToolsStrictIsOff(t *testing.T) {
	home := syncHome(t)
	if err := provider.Save(provider.Provider{ID: "resp", Name: "Resp", Key: "k", Responses: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("resp", "http://127.0.0.1:1/v1", []catalog.Model{{ID: "gpt-5.5", Efforts: []string{"high"}}}); err != nil {
		t.Fatal(err)
	}
	piModels := filepath.Join(home, ".pi", "agent", "models.json")
	// the provider block as uni-switch wrote it before this provider was, with
	// a key of pi-cache-optimizer's in it
	writeFile(t, piModels, `{
  "providers": {
    "uni-switch": {
      "api": "openai-completions",
      "apiKey": "uni-switch",
      "baseUrl": "http://127.0.0.1:3425/v1",
      "compat": {
        "sendSessionAffinityHeaders": true
      },
      "models": [
        {
          "id": "gone/old-model",
          "name": "Old"
        }
      ],
      "name": "uni-switch"
    }
  }
}
`)
	// the models.json the test writes is one uni-switch wrote while Pi was
	// connected: a sync only tends a ListOnly agent's file while it
	// reads as connected (Connect), and a block uni-switch never set up is
	// the user's and is left alone
	if err := pi(home).Connect(); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	b, err := os.ReadFile(piModels)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Providers struct {
			UniSwitch struct {
				Models []struct {
					ID     string         `json:"id"`
					API    string         `json:"api"`
					Compat map[string]any `json:"compat"`
				} `json:"models"`
			} `json:"uni-switch"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%v:\n%s", err, b)
	}
	strict := map[string]bool{}
	for _, m := range f.Providers.UniSwitch.Models {
		if m.API == "openai-responses" && m.Compat["supportsStrictMode"] != true {
			t.Fatalf("%s is asked on Responses and isn't told strict is off:\n%s", m.ID, b)
		}
		strict[m.ID] = m.Compat["supportsStrictMode"] == true
	}
	if !strict["resp/gpt-5.5"] {
		t.Fatalf("no responses model of uni-switch's carries supportsStrictMode:\n%s", b)
	}
	if strict["relay/glm-4.6"] {
		t.Fatalf("a Chat model was told strict is off:\n%s", b)
	}
}
