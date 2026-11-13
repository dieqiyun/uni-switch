package agent

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// pencilUser is ~/.pencil/models.json as Pencil writes a custom provider
// (徐小小 on Discord), with a key and a top-level key besides.
const pencilUser = `{
  "providers": {
    "XXXXX": {
      "name": "XXX",
      "baseUrl": "https://relay.example/v1",
      "api": "openai-responses",
      "apiKey": "sk-users-own",
      "models": [
        {
          "id": "gpt-6-astra",
          "name": "gpt-6-astra",
          "reasoning": true,
          "input": ["text", "image"],
          "cost": {"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
          "contextWindow": 1000000,
          "maxTokens": 1000000,
          "compat": {}
        }
      ]
    }
  },
  "somethingOfPencils": {"kept": true}
}
`

// pencilModel is a model of Pencil's custom provider, every field it
// writes for one required and nothing else taken.
type pencilModel struct {
	ID            *string            `json:"id"`
	Name          *string            `json:"name"`
	Reasoning     *bool              `json:"reasoning"`
	Input         []string           `json:"input"`
	Cost          map[string]float64 `json:"cost"`
	ContextWindow *int               `json:"contextWindow"`
	MaxTokens     *int               `json:"maxTokens"`
	Compat        map[string]any     `json:"compat"`
	// Pi's, which Pencil's own editor never writes; allowed, as Pi reads it
	ThinkingLevelMap map[string]any `json:"thinkingLevelMap"`
}

type pencilProvider struct {
	Name    string        `json:"name"`
	BaseURL string        `json:"baseUrl"`
	API     string        `json:"api"`
	APIKey  string        `json:"apiKey"`
	Models  []pencilModel `json:"models"`
}

// pencilFormat checks raw is a provider Pencil reads: one of its three
// APIs for the whole provider, and each model with every field Pencil
// writes, of its type, and no field of its own (a model's api or baseUrl).
func pencilFormat(t *testing.T, raw []byte) pencilProvider {
	t.Helper()
	var p pencilProvider
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		t.Fatalf("not Pencil's format: %v\n%s", err, raw)
	}
	if !slices.Contains([]string{"openai-completions", "openai-responses", "anthropic-messages"}, p.API) {
		t.Errorf("api %q", p.API)
	}
	if p.Name == "" || p.BaseURL == "" || len(p.Models) == 0 {
		t.Errorf("provider: %s", raw)
	}
	for _, m := range p.Models {
		switch {
		case m.ID == nil || *m.ID == "" || m.Name == nil || m.Reasoning == nil:
			t.Errorf("model without id, name or reasoning: %s", raw)
		case len(m.Input) == 0 || slices.ContainsFunc(m.Input, func(s string) bool { return s != "text" && s != "image" }):
			t.Errorf("%s: input %v", *m.ID, m.Input)
		case len(m.Cost) != 4 || !reflect.DeepEqual(slices.Sorted(maps.Keys(m.Cost)), []string{"cacheRead", "cacheWrite", "input", "output"}):
			t.Errorf("%s: cost %v", *m.ID, m.Cost)
		case m.ContextWindow == nil || *m.ContextWindow <= 0 || m.MaxTokens == nil || *m.MaxTokens <= 0 || *m.MaxTokens > *m.ContextWindow:
			t.Errorf("%s: window %v, out %v", *m.ID, m.ContextWindow, m.MaxTokens)
		case m.Compat == nil:
			t.Errorf("%s: no compat", *m.ID)
		}
	}
	return p
}

// Pencil (pen.dev) reads custom providers from ~/.pencil/models.json in
// Pi's format. uni-switch adds itself as the provider "uni-switch", every model on
// openai-completions at the gateway, and keeps the user's own providers,
// keys and keys of Pencil's; off, it takes only its own out.
func TestPencilModels(t *testing.T) {
	home := syncHome(t)
	for _, p := range []provider.Provider{
		// asked natively, Pi would name another api and baseUrl per model
		{ID: "anth", Name: "Anth", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-sonnet-5"}},
		{ID: "resp", Name: "Resp", Key: "k", Responses: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(home, ".pencil")
	path := filepath.Join(dir, "models.json")

	a := pencil(home)
	if a.Detected() {
		t.Fatal("found with no ~/.pencil")
	}
	if !slices.ContainsFunc(All(), func(x *Agent) bool { return x.ID == "pencil" && x.Name == "Pencil" }) {
		t.Fatal("Pencil is not among the agents")
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(path, []byte(pencilUser), 0o600)
	if !a.Detected() {
		t.Fatal("not found with ~/.pencil")
	}
	f := a.Field("provider")
	if f == nil {
		t.Fatal("no provider field")
	}
	if f.Get() != "" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if f.Get() != uniSwitchID {
		t.Fatalf("get: %q", f.Get())
	}

	var file, user map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(path)), &file); err != nil {
		t.Fatalf("%v\n%s", err, readFile(path))
	}
	json.Unmarshal([]byte(pencilUser), &user)
	var provs, userProvs map[string]json.RawMessage
	json.Unmarshal(file["providers"], &provs)
	json.Unmarshal(user["providers"], &userProvs)
	if !sameJSON(string(provs["XXXXX"]), json.RawMessage(userProvs["XXXXX"])) {
		t.Errorf("the user's provider changed:\n%s", provs["XXXXX"])
	}
	if !sameJSON(string(file["somethingOfPencils"]), json.RawMessage(user["somethingOfPencils"])) {
		t.Errorf("Pencil's own key changed: %s", file["somethingOfPencils"])
	}

	p := pencilFormat(t, provs[uniSwitchID])
	if p.API != "openai-completions" || p.BaseURL != gatewayV1() || p.APIKey != gateway.TokenFor("pencil") {
		t.Errorf("provider: %s", provs[uniSwitchID])
	}
	ids := []string{}
	for _, m := range p.Models {
		ids = append(ids, *m.ID)
	}
	for _, id := range []string{"relay/glm-4.6", "anth/claude-sonnet-5", "resp/gpt-5.5"} {
		if !slices.Contains(ids, id) {
			t.Errorf("%s not listed: %v", id, ids)
		}
	}

	// the catalog sync keeps it, and the user's
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(readFile(path)), &file)
	json.Unmarshal(file["providers"], &provs)
	pencilFormat(t, provs[uniSwitchID])
	if _, ok := provs["XXXXX"]; !ok {
		t.Fatal("the user's provider is gone after a sync")
	}

	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" {
		t.Fatalf("get after off: %q", f.Get())
	}
	file, provs = nil, nil
	json.Unmarshal([]byte(readFile(path)), &file)
	json.Unmarshal(file["providers"], &provs)
	if _, ok := provs[uniSwitchID]; ok || !sameJSON(string(provs["XXXXX"]), json.RawMessage(userProvs["XXXXX"])) {
		t.Errorf("after off: %s", readFile(path))
	}
	// off, a sync leaves it off
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" {
		t.Fatalf("sync put it back: %s", readFile(path))
	}
}
