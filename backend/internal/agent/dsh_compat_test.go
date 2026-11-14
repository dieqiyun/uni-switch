package agent

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// dshRouteCompat is each model's compat on route key of a patch list's
// llm-pi-ai entry, every switch in it, the ones uni-switch doesn't know too.
func dshRouteCompat(t *testing.T, path, key string) (models []string, compat map[string]map[string]any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list []struct {
		ID     string `yaml:"id"`
		Config struct {
			Providers map[string]struct {
				Models []struct {
					ID     string         `yaml:"id"`
					Name   string         `yaml:"name"`
					Compat map[string]any `yaml:"compat"`
				} `yaml:"models"`
			} `yaml:"providers"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(b, &list); err != nil {
		t.Fatalf("%v:\n%s", err, b)
	}
	compat = map[string]map[string]any{}
	for _, it := range list {
		if it.ID != dshPiRow {
			continue
		}
		for _, m := range it.Config.Providers[key].Models {
			models = append(models, m.ID+"="+m.Name)
			if m.Compat != nil {
				compat[m.ID] = m.Compat
			}
		}
	}
	return models, compat
}

// #2504 (h4rk8s): a compat switch set by hand in dsh's Models page — a
// maxTokensField a provider needs — went on every refresh of uni-switch's model
// list, and a model that worked failed again. It now stays on the same model
// of the same route on the same protocol, while the name and limits are
// written as the catalog has them; a model on another route doesn't take it.
func TestDshRefreshKeepsHandSetCompat(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	dshKeyFixture(dir)
	route := func(key, api string) string {
		return "      " + key + ":\n        displayName: uni-switch\n        apiKeyEnv: " + dshKeyRef + "\n        api: " + api + "\n        baseURL: " + gatewayV1() + "\n        models:\n" +
			"          - id: deepseek/pro\n            name: stale\n            contextWindow: 1\n            compat:\n              maxTokensField: max_tokens\n              supportsStore: false\n"
	}
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"+
		"- id: llm-pi-ai\n  name: \"@deepseek-ai/dsh-llm-pi-ai\"\n  config:\n    providers:\n"+
		route("uni-switch", "openai-completions")+route("uni-switch-responses", "openai-responses")+
		"- id: agent-default-model\n  config:\n    provider: uni-switch\n    model: deepseek/pro\n"), 0o644)
	a := dsh(home)
	for i := 0; i < 2; i++ {
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(web)
		models, compat := dshRouteCompat(t, web, "uni-switch")
		if len(models) == 0 || strings.Contains(strings.Join(models, ","), "stale") {
			t.Fatalf("the route wasn't written as the catalog has it: %v\n%s", models, b)
		}
		if c := compat["deepseek/pro"]; c["maxTokensField"] != "max_tokens" || c["supportsStore"] != false || len(c) != 2 {
			t.Fatalf("sync %d: the hand-set compat went: %v\n%s", i+1, compat, b)
		}
		if c, ok := compat["deepseek/flash"]; ok {
			t.Fatalf("a model new to the route took another's compat: %v", c)
		}
		if strings.Contains(string(b), "uni-switch-responses:") {
			// the route went with its last model, so its compat can't
			// follow the model onto another one
			t.Fatalf("a route with none of uni-switch's models stayed:\n%s", b)
		}
	}
}

// A route whose protocol changes doesn't keep the old protocol's compat.
func TestDshKeepCompatNotAcrossProtocols(t *testing.T) {
	var cur, route yaml.Node
	yaml.Unmarshal([]byte("api: openai-completions\nmodels:\n  - id: m\n    compat:\n      maxTokensField: max_tokens\n"), &cur)
	yaml.Unmarshal([]byte("api: anthropic-messages\nmodels:\n  - id: m\n"), &route)
	dshKeepCompat(cur.Content[0], route.Content[0])
	out, _ := yaml.Marshal(route.Content[0])
	if strings.Contains(string(out), "maxTokensField") {
		t.Fatalf("compat crossed a protocol change:\n%s", out)
	}
	yaml.Unmarshal([]byte("api: openai-completions\nmodels:\n  - id: m\n  - id: n\n"), &route)
	dshKeepCompat(cur.Content[0], route.Content[0])
	out, _ = yaml.Marshal(route.Content[0])
	if strings.Count(string(out), "maxTokensField") != 1 {
		t.Fatalf("same protocol, same model:\n%s", out)
	}
}
