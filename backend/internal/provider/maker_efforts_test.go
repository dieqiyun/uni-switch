package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// makerCatalog is models.dev as it had mimo-v2.6-flash (#214): Xiaomi's own
// entry with a thinking switch alone, resellers giving levels up to max.
const makerCatalog = `{
  "xiaomi-token-plan-cn": {"models": {
    "mimo-v2.6-flash": {"id":"mimo-v2.6-flash","reasoning":true,"reasoning_options":[{"type":"toggle"}]},
    "mimo-v2.6-flash-preview": {"id":"mimo-v2.6-flash-preview","reasoning":true,"reasoning_options":[{"type":"toggle"}]}}},
  "xiaomi": {"models": {
    "mimo-v2.6-flash": {"id":"mimo-v2.6-flash","reasoning":true,"reasoning_options":[{"type":"toggle"}]}}},
  "llmgateway": {"models": {
    "mimo-v2.6-flash": {"id":"mimo-v2.6-flash","reasoning_options":[{"type":"effort","values":["none","minimal","low","medium","high","xhigh","max"]}]},
    "open-model": {"id":"open-model","reasoning_options":[{"type":"effort","values":["low","medium","high","max"]}]}}},
  "requesty": {"models": {
    "mimo-v2.6-flash": {"id":"mimo-v2.6-flash","reasoning_options":[{"type":"effort","values":["none","low","medium","high","max"]}]},
    "open-model": {"id":"open-model","reasoning_options":[{"type":"effort","values":["low","medium","high","max"]}]}}},
  "anthropic": {"models": {
    "claude-sonnet-4-5": {"id":"claude-sonnet-4-5","reasoning":true,"reasoning_options":[{"type":"budget_tokens"}]}}},
  "relay-x": {"models": {
    "claude-sonnet-4-5": {"id":"claude-sonnet-4-5","reasoning_options":[{"type":"effort","values":["low","medium","high"]}]}}}
}`

func makerHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(makerCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
}

// A model whose maker lists it with a thinking switch alone takes no levels,
// whoever serves it: Xiaomi's preset, a model of Xiaomi's its list leaves
// out, or a relay of the user's serving it — none borrows the resellers'
// levels up to max, which Xiaomi turns away (#214). A model no maker lists
// still takes the resellers' levels, and one budgeted by its maker too.
func TestMakerLevelsBeforeResellers(t *testing.T) {
	makerHome(t)
	xiaomi, err := legacyVendor("xiaomi")
	if err != nil {
		t.Fatal(err)
	}
	xiaomi.Key, xiaomi.Models = "k", []string{"mimo-v2.6-flash", "mimo-v2.6-flash-preview"}
	if err := Save(xiaomi); err != nil {
		t.Fatal(err)
	}
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: "https://relay.test/v1", Key: "k",
		Models: []string{"mimo-v2.6-flash", "xiaomi/MiMo-V2.6-Flash", "open-model", "claude-sonnet-4-5", "mystery"}}); err != nil {
		t.Fatal(err)
	}
	// the relay's own list, fetched: ids models.dev has under no vendor of its
	if err := catalog.SaveLive("relay", "https://relay.test/v1", []catalog.Model{
		{ID: "mimo-v2.6-flash"}, {ID: "xiaomi/MiMo-V2.6-Flash"}, {ID: "open-model"}, {ID: "claude-sonnet-4-5"}, {ID: "mystery"}}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range Catalog() {
		got[e.ID] = strings.Join(e.Efforts, ",")
	}
	for id, want := range map[string]string{
		"xiaomi/mimo-v2.6-flash":         "",
		"xiaomi/mimo-v2.6-flash-preview": "",
		"relay/mimo-v2.6-flash":          "",
		"relay/xiaomi/MiMo-V2.6-Flash":   "",
		"relay/open-model":               "low,medium,high,max",
		"relay/claude-sonnet-4-5":        "low,medium,high",
		"relay/mystery":                  "",
	} {
		if g, ok := got[id]; !ok || g != want {
			t.Errorf("%s: levels %q (listed %v), want %q", id, g, ok, want)
		}
	}
	for _, c := range []struct {
		p, model, known string
		levelless       bool
	}{
		{"xiaomi", "mimo-v2.6-flash", "", true},
		{"xiaomi", "mimo-v2.6-flash-preview", "", true}, // left out of the list
		{"xiaomi", "mimo-v2.6-flash-free", "", false},   // no one lists it
		{"relay", "mimo-v2.6-flash", "", true},
		{"relay", "open-model", "low,medium,high,max", false},
		{"relay", "claude-sonnet-4-5", "low,medium,high", false},
		{"relay", "mystery", "", false},
	} {
		p, err := Find(c.p)
		if err != nil {
			t.Fatal(err)
		}
		if k := strings.Join(p.Known(c.model), ","); k != c.known {
			t.Errorf("%s/%s: Known %q, want %q", c.p, c.model, k, c.known)
		}
		if l := p.Levelless(c.model); l != c.levelless {
			t.Errorf("%s/%s: Levelless %v, want %v", c.p, c.model, l, c.levelless)
		}
	}
}
