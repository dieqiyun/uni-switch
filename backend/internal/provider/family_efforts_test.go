package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
)

// newModelCatalog is models.dev as it listed gpt-6.1-sol the day it was
// added: OpenAI's entry says it thinks, with no levels yet, while its
// family's gpt-6-sol and gpt-6-astra have theirs; Xiaomi's mimo line takes
// a thinking switch alone.
const newModelCatalog = `{
  "openai": {"models": {
    "gpt-6.1-sol": {"id":"gpt-6.1-sol","family":"gpt-sol","reasoning":true,"release_date":"2026-09-29"},
    "gpt-6-sol": {"id":"gpt-6-sol","family":"gpt-sol","reasoning":true,"release_date":"2026-09-22","reasoning_options":[{"type":"effort","values":["none","low","medium","high","xhigh","max"]}]},
    "gpt-5.6-sol": {"id":"gpt-5.6-sol","family":"gpt-sol","reasoning":true,"release_date":"2026-07-09","reasoning_options":[{"type":"effort","values":["none","low","medium","high","xhigh"]}]},
    "gpt-6-astra": {"id":"gpt-6-astra","family":"gpt-astra","reasoning":true,"release_date":"2026-09-04","reasoning_options":[{"type":"effort","values":["low","medium","high","xhigh","max"]}]},
    "gpt-4.1": {"id":"gpt-4.1","family":"gpt","reasoning":false,"release_date":"2025-04-14"}}},
  "xiaomi": {"models": {
    "mimo-v2.6-flash": {"id":"mimo-v2.6-flash","family":"mimo","reasoning":true,"release_date":"2026-09-22","reasoning_options":[{"type":"toggle"}]},
    "mimo-v2.5": {"id":"mimo-v2.5","family":"mimo","reasoning":true,"release_date":"2026-04-22"}}},
  "requesty": {"models": {
    "gpt-6.1-sol": {"id":"gpt-6.1-sol","reasoning":true}}}
}`

func newModelHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(newModelCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
}

// A model models.dev has just added, listed as thinking but without its
// levels yet, offers its family's (NextCommit on Discord: a relay's
// gpt-6.1-sol had no effort picker in Codex, gpt-6-astra had one), and so
// does one newer than the catalog on this machine. A family that takes a
// thinking switch alone still offers none (#214), and a model that doesn't
// reason none either.
func TestNewModelTakesItsFamilysLevels(t *testing.T) {
	newModelHome(t)
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: "https://relay.test/v1", Key: "k",
		Models: []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6.2-sol", "mimo-v2.6-flash", "mimo-v2.7-flash", "gpt-4.1"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("relay", "https://relay.test/v1", []catalog.Model{
		{ID: "gpt-6.1-sol"}, {ID: "gpt-6-astra"}, {ID: "gpt-6.2-sol"}, {ID: "mimo-v2.6-flash"}, {ID: "mimo-v2.7-flash"}, {ID: "gpt-4.1"}}); err != nil {
		t.Fatal(err)
	}
	openai, err := legacyVendor("openai")
	if err != nil {
		t.Fatal(err)
	}
	openai.Key, openai.Models = "k", []string{"gpt-6.1-sol"}
	if err := Save(openai); err != nil {
		t.Fatal(err)
	}
	sol := "none,low,medium,high,xhigh,max"
	got := map[string]string{}
	for _, e := range Catalog() {
		// without Codex's ultra, added where OpenAI offers it (ultra.go)
		got[e.ID] = strings.TrimSuffix(strings.Join(e.Efforts, ","), ",ultra")
	}
	for id, want := range map[string]string{
		"relay/gpt-6.1-sol":        sol,
		"relay/gpt-6-astra":        "low,medium,high,xhigh,max",
		"relay/gpt-6.2-sol":        sol, // newer than the catalog
		"relay/mimo-v2.6-flash":    "",
		"relay/mimo-v2.7-flash":    "",
		"relay/gpt-4.1":            "",
		openai.ID + "/gpt-6.1-sol": sol,
	} {
		if g, ok := got[id]; !ok || g != want {
			t.Errorf("%s: levels %q (listed %v), want %q", id, g, ok, want)
		}
	}
	relay, err := Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if k := strings.Join(relay.Known("gpt-6.1-sol"), ","); k != sol {
		t.Errorf("Known(gpt-6.1-sol) = %q, want %q", k, sol)
	}
	if relay.Levelless("gpt-6.1-sol") {
		t.Error("gpt-6.1-sol reads as having no levels")
	}
	if !relay.Levelless("mimo-v2.6-flash") {
		t.Error("mimo-v2.6-flash reads as having levels")
	}

	// what Codex is handed: the picker's levels and a default
	var cat struct {
		Models []struct {
			Slug   string `json:"slug"`
			Levels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(codexcat.Catalog(CodexListed()), &cat); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range cat.Models {
		if m.Slug != "relay/gpt-6.1-sol" {
			continue
		}
		found = true
		var ls []string
		for _, l := range m.Levels {
			ls = append(ls, l.Effort)
		}
		if strings.TrimSuffix(strings.Join(ls, ","), ",ultra") != sol {
			t.Errorf("Codex's levels for relay/gpt-6.1-sol: %v, want %s", ls, sol)
		}
	}
	if !found {
		t.Error("relay/gpt-6.1-sol isn't in Codex's catalog")
	}
}
