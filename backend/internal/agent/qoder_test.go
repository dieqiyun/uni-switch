package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tidwall/jsonc"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

func TestQoder(t *testing.T) {
	t.Run("global", func(t *testing.T) { testQoder(t, qoder, filepath.Join(".qoder", "settings.json")) })
	t.Run("cn", func(t *testing.T) { testQoder(t, qoderCN, filepath.Join(".qoder-cn", "settings.json")) })
	// Qoder CN's own config dir, apart from Qoder's
	home := t.TempDir()
	t.Setenv("QODER_CONFIG_DIR", filepath.Join(home, "g"))
	t.Setenv("QODERCN_CONFIG_DIR", filepath.Join(home, "c"))
	if g, c := qoder(home), qoderCN(home); g.Path != filepath.Join(home, "g", "settings.json") ||
		c.Path != filepath.Join(home, "c", "settings.json") || c.ID != "qoder-cn" || c.Bin != "qoderclicn" {
		t.Fatalf("paths %q %q", g.Path, c.Path)
	}
}

func testQoder(t *testing.T, mk func(string) *Agent, rel string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("QODER_CONFIG_DIR", "")
	t.Setenv("QODERCN_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash", "v4.1-flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, rel)
	type file struct {
		Providers map[string]map[string]any `json:"providers"`
		Model     map[string]any            `json:"model"`
		Theme     string                    `json:"theme"`
	}
	read := func() (file, string) {
		var f file
		b, _ := os.ReadFile(path)
		json.Unmarshal(jsonc.ToJSON(b), &f)
		return f, string(b)
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{
  // the user's
  "theme": "dark",
  "providers": {"mine": {"baseUrl": "https://x/v1", "apiKey": "sk-m", "model": "m1"}},
  "model": {"name": "performance", "reasoningEffort": "high"}
}`), 0o644)

	a := mk(home)
	f, e := a.Field("model"), a.Field("effort")
	if f.Get() != "performance" || e.Get() != "high" {
		t.Fatalf("get: %q %q", f.Get(), e.Get())
	}
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	c, raw := read()
	p := c.Providers[uniSwitchID]
	if !strings.Contains(raw, "// the user's") || c.Model["name"] != "uni-switch/deepseek/pro" || p["baseUrl"] != gatewayV1() || p["apiKey"] != gateway.TokenFor(a.ID) ||
		p["protocol"] != "openai" || p["model"] != "deepseek/pro" || c.Providers["mine"]["apiKey"] != "sk-m" || c.Theme != "dark" {
		t.Fatalf("uni-switch:\n%s", raw)
	}
	var ids []string
	for _, m := range p["models"].([]any) {
		ids = append(ids, m.(map[string]any)["model"].(string))
	}
	if len(ids) < 2 || a.Check() != "" {
		t.Fatalf("models %v, check %q", ids, a.Check())
	}
	// another uni-switch model keeps the model stashed first
	if err := f.Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if err := e.Set("low"); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if c.Providers[uniSwitchID]["model"] != "deepseek/flash" || c.Model["reasoningEffort"] != "low" {
		t.Fatalf("flash:\n%s", raw)
	}
	// the effort is the model's own, in model.preferences: Qoder asks
	// reasoningEffort only of a model with none there
	pref := func(model string) any {
		c, _ := read()
		ps, _ := c.Model["preferences"].(map[string]any)
		p, _ := ps[model].(map[string]any)
		r, _ := p["reasoning"].(map[string]any)
		return r["effort"]
	}
	if pref("uni-switch/deepseek/flash") != "low" {
		t.Fatalf("flash preference:\n%s", raw)
	}
	// Qoder moved reasoningEffort into a preference of the model's own, and
	// it was turned off: that is the effort shown, and the one set
	edit.DelJSON(path, "model.reasoningEffort")
	edit.SetJSON(path, edit.KV{Path: "model.preferences", Value: map[string]any{
		"uni-switch/deepseek/flash": map[string]any{"reasoning": map[string]any{"effort": "high", "enabled": false}, "contextWindow": 1000},
		"performance":               map[string]any{"reasoning": map[string]any{"effort": "max"}}}})
	if e.Get() != "high" {
		t.Fatalf("preference get: %q", e.Get())
	}
	if err := e.Set("medium"); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if p := c.Model["preferences"].(map[string]any)["uni-switch/deepseek/flash"].(map[string]any); pref("uni-switch/deepseek/flash") != "medium" ||
		p["reasoning"].(map[string]any)["enabled"] != nil || p["contextWindow"] != 1000.0 || pref("performance") != "max" || c.Model["reasoningEffort"] != "medium" {
		t.Fatalf("preference set:\n%s", raw)
	}
	// a model with no effort of its own (a dot in its id) takes the one in use
	if err := f.Set("uni-switch/deepseek/v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	if pref("uni-switch/deepseek/v4.1-flash") != "medium" || e.Get() != "medium" {
		_, raw = read()
		t.Fatalf("carried effort:\n%s", raw)
	}
	f.Set("uni-switch/deepseek/flash")

	// reset: uni-switch's provider goes and the user's model comes back
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if _, ok := c.Providers[uniSwitchID]; ok || c.Model["name"] != "performance" || c.Providers["mine"] == nil {
		t.Fatalf("reset:\n%s", raw)
	}

	// a model of Qoder's own, from uni-switch, takes uni-switch's provider out too
	f.Set("uni-switch/deepseek/pro")
	if err := f.Set("ultimate"); err != nil {
		t.Fatal(err)
	}
	c, raw = read()
	if _, ok := c.Providers[uniSwitchID]; ok || c.Model["name"] != "ultimate" {
		t.Fatalf("own:\n%s", raw)
	}

	// wired before Qoder had a key of its own: still uni-switch's, and a sync
	// gives it Qoder's key, which the gateway knows it by (not Bun's UA)
	f.Set("uni-switch/deepseek/pro")
	edit.SetJSON(path, edit.KV{Path: "providers." + uniSwitchID + ".apiKey", Value: gateway.Token})
	if a.Check() != "" {
		t.Fatalf("old key: %q", a.Check())
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if c, raw = read(); c.Providers[uniSwitchID]["apiKey"] != gateway.TokenFor(a.ID) || usage.AgentOf(a.ID) != a.ID {
		t.Fatalf("synced key:\n%s", raw)
	}

	// no settings before: a new file with uni-switch's provider
	os.Remove(path)
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if c, raw = read(); c.Model["name"] != "uni-switch/deepseek/pro" {
		t.Fatalf("new file:\n%s", raw)
	}
}

// What the user set on uni-switch's provider in Qoder's settings stays through
// every sync and pick, as T3 Code's did after #2030: its name, the model
// routing that picks compact's and subagents' models, its timeouts and auth
// type. The address, key, model and model list stay uni-switch's.
func TestQoderKeepsTheUsersSettings(t *testing.T) {
	for _, b := range []struct {
		mk  func(string) *Agent
		rel string
	}{{qoder, filepath.Join(".qoder", "settings.json")}, {qoderCN, filepath.Join(".qoder-cn", "settings.json")}} {
		t.Run(b.rel, func(t *testing.T) {
			home := syncHome(t)
			t.Setenv("QODER_CONFIG_DIR", "")
			t.Setenv("QODERCN_CONFIG_DIR", "")
			path := filepath.Join(home, b.rel)
			os.MkdirAll(filepath.Dir(path), 0o700)
			os.WriteFile(path, []byte(`{"providers":{"mine":{"baseUrl":"https://x/v1","apiKey":"sk-m","model":"m1"}},"model":{"name":"m1"}}`), 0o600)
			a := b.mk(home)
			f := a.Field("model")
			if err := f.Set("uni-switch/relay/glm-4.6"); err != nil {
				t.Fatal(err)
			}
			// the user edits uni-switch's provider, as they would Qoder's settings
			if err := edit.SetJSON(path,
				edit.KV{Path: "providers.uni-switch.displayName", Value: "uni-switch GW"},
				edit.KV{Path: "providers.uni-switch.routing", Value: map[string]any{"compact": "relay/glm-4.6", "subagent": "relay/glm-4.6"}},
				edit.KV{Path: "providers.uni-switch.streamIdleTimeoutMs", Value: 600000},
				edit.KV{Path: "providers.uni-switch.firstPayloadTimeoutMs", Value: 120000},
				edit.KV{Path: "providers.uni-switch.authType", Value: "bearer"},
				edit.KV{Path: "providers.uni-switch.baseUrl", Value: "http://127.0.0.1:1/v1"},
				edit.KV{Path: "providers.uni-switch.models", Value: []any{map[string]any{"model": "gone/old"}}},
			); err != nil {
				t.Fatal(err)
			}
			// a provider added since, so the sync has a model list to change
			if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"pro"}}); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err := a.Sync(); err != nil {
					t.Fatal(err)
				}
			}
			check := func(when string) {
				t.Helper()
				var c struct {
					Providers map[string]struct {
						DisplayName           string            `json:"displayName"`
						Routing               map[string]string `json:"routing"`
						StreamIdleTimeoutMs   int               `json:"streamIdleTimeoutMs"`
						FirstPayloadTimeoutMs int               `json:"firstPayloadTimeoutMs"`
						AuthType              string            `json:"authType"`
						BaseURL               string            `json:"baseUrl"`
						APIKey                string            `json:"apiKey"`
						Models                []struct {
							Model string `json:"model"`
						} `json:"models"`
					} `json:"providers"`
				}
				raw := readFile(path)
				if err := json.Unmarshal([]byte(raw), &c); err != nil {
					t.Fatalf("%v\n%s", err, raw)
				}
				p := c.Providers[uniSwitchID]
				if p.DisplayName != "uni-switch GW" || p.Routing["compact"] != "relay/glm-4.6" || p.Routing["subagent"] != "relay/glm-4.6" ||
					p.StreamIdleTimeoutMs != 600000 || p.FirstPayloadTimeoutMs != 120000 || p.AuthType != "bearer" {
					t.Errorf("%s: the user's settings not kept:\n%s", when, raw)
				}
				var ids []string
				for _, m := range p.Models {
					ids = append(ids, m.Model)
				}
				if p.BaseURL == "http://127.0.0.1:1/v1" || p.BaseURL == "" || p.APIKey == "" || slices.Contains(ids, "gone/old") || !slices.Contains(ids, "deepseek/pro") {
					t.Errorf("%s: uni-switch's own parts not written: %s %s %v", when, p.BaseURL, p.APIKey, ids)
				}
				if c.Providers["mine"].APIKey != "sk-m" {
					t.Errorf("%s: the user's own provider changed:\n%s", when, raw)
				}
			}
			check("sync")
			if err := f.Set("uni-switch/deepseek/pro"); err != nil {
				t.Fatal(err)
			}
			check("pick")
		})
	}
}
