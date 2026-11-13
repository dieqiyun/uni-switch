package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestOpenCodeVariants(t *testing.T) {
	for _, c := range []struct {
		efforts []string
		want    string
	}{
		{[]string{"none", "high", "max"}, `{"none":{"reasoningEffort":"none"},"high":{"reasoningEffort":"high"},"max":{"reasoningEffort":"max"}}`},
		{[]string{"low", "medium", "high", "xhigh"}, `{"low":{"reasoningEffort":"low"},"medium":{"reasoningEffort":"medium"},"high":{"reasoningEffort":"high"},"xhigh":{"reasoningEffort":"xhigh"}}`},
		// weakest first whatever order the model lists them in (#713)
		{[]string{"max", "ultra", "low", "xhigh", "medium", "high"}, `{"low":{"reasoningEffort":"low"},"medium":{"reasoningEffort":"medium"},"high":{"reasoningEffort":"high"},"xhigh":{"reasoningEffort":"xhigh"},"max":{"reasoningEffort":"max"},"ultra":{"reasoningEffort":"ultra"}}`},
		// none at all: an empty set, so OpenCode 2 doesn't make low, medium
		// and high of its own
		{nil, `{}`},
	} {
		b, err := json.Marshal(openCodeVariants(c.efforts, false))
		if err != nil || string(b) != c.want {
			t.Errorf("%v: got %s (%v), want %s", c.efforts, b, err, c.want)
		}
	}
	// a model Codex's /fast is asked of: each level again, fast, after them,
	// the tier both as OpenCode 1 sends an option (service_tier as it is)
	// and as OpenCode 2 does (extraBody); fast alone with no levels (#2379)
	for _, c := range []struct {
		efforts []string
		want    string
	}{
		{[]string{"high", "low"}, `{"low":{"reasoningEffort":"low"},"high":{"reasoningEffort":"high"},` +
			`"low-fast":{"extraBody":{"service_tier":"priority"},"reasoningEffort":"low","service_tier":"priority"},` +
			`"high-fast":{"extraBody":{"service_tier":"priority"},"reasoningEffort":"high","service_tier":"priority"}}`},
		{nil, `{"fast":{"extraBody":{"service_tier":"priority"},"service_tier":"priority"}}`},
	} {
		b, err := json.Marshal(openCodeVariants(c.efforts, true))
		if err != nil || string(b) != c.want {
			t.Errorf("fast %v: got %s (%v), want %s", c.efforts, b, err, c.want)
		}
	}
}

// variantOrder is the variants of the one model in an opencode.json, in the
// order the file's bytes list them.
func variantOrder(t *testing.T, b []byte) []string {
	t.Helper()
	i := bytes.Index(b, []byte(`"variants"`))
	if i < 0 {
		t.Fatalf("no variants: %s", b)
	}
	rest := b[i:]
	at := map[string]int{}
	var got []string
	for _, e := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
		if j := bytes.Index(rest, []byte(`"`+e+`":`)); j >= 0 {
			at[e] = j
			got = append(got, e)
		}
	}
	slices.SortFunc(got, func(x, y string) int { return at[x] - at[y] })
	return got
}

// TestOpenCodeVariantsInOrder: opencode.json lists a model's variants weakest
// first, as OpenCode and OpenChamber show them in the file's order; they
// were alphabetical — high, low, max, medium, ultra, xhigh (#713). A file an
// older uni-switch wrote so is put in order by the next sync, and one in order
// is left alone.
func TestOpenCodeVariantsInOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{
		ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "key", Models: []string{"deep"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "deep", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeFile(t, path, "{\n  \"theme\": \"dark\"\n}\n")
	oc := opencode(home, filepath.Join(home, ".config"))
	if err := oc.Field("model").Set("uni-switch/think/deep"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if got := variantOrder(t, b); !slices.Equal(got, want) {
		t.Fatalf("written: %v, want %v\n%s", got, want, b)
	}

	// as an older uni-switch wrote it: the same block from a map, keys sorted
	var block map[string]any
	if err := json.Unmarshal([]byte(mustJSON(t, uniSwitchProviderJSON("opencode"))), &block); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path, edit.KV{Path: "provider.uni-switch", Value: block}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if got := variantOrder(t, b); !slices.Equal(got, []string{"high", "low", "max", "medium", "ultra", "xhigh"}) {
		t.Fatalf("old block: %v\n%s", got, b)
	}
	if err := oc.Sync(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if got := variantOrder(t, b); !slices.Equal(got, want) {
		t.Fatalf("synced: %v, want %v\n%s", got, want, b)
	}
	if err := oc.Sync(); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(path); !bytes.Equal(b, b2) {
		t.Fatalf("rewritten though in order:\n%s\n%s", b, b2)
	}
}

// modelKeys are the ids of uni-switch's models in an opencode config at path,
// in the order the file's bytes list them.
func modelKeys(t *testing.T, path string) []string {
	t.Helper()
	raw, ok := edit.GetJSON(path, "provider.uni-switch.models")
	if !ok {
		t.Fatalf("no models of uni-switch's in %s", path)
	}
	var out []string
	for _, p := range objectPairs([]byte(raw)) {
		out = append(out, p.k)
	}
	return out
}

// TestOpenCodeModelsInOrder: opencode.json lists uni-switch's models in the
// order the user dragged them to on the Agents page (#4575); they were
// alphabetical — the models were a map, whose keys marshal sorted, and a
// pure re-ordering wasn't even written, the file and the new block sorting
// alike. The variants under each model kept theirs since #713.
func TestOpenCodeModelsInOrder(t *testing.T) {
	home := syncHome(t)
	// a provider the catalog lists after relay's, whose models sort before:
	// uni-switch's own order and the alphabetical one differ
	if err := provider.Save(provider.Provider{ID: "aaa", Name: "AAA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeFile(t, path, `{"theme":"dark"}`)
	if err := opencode(home, filepath.Join(home, ".config")).Field("model").Set("uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	want := []string{"relay/glm-4.6", "aaa/m1"}
	if got := modelKeys(t, path); !slices.Equal(got, want) {
		t.Fatalf("written: %v, want %v\n%s", got, want, readFile(path))
	}
	if err := provider.SetModelOrder("opencode", []string{"aaa/m1"}); err != nil {
		t.Fatal(err)
	}
	if err := opencode(home, filepath.Join(home, ".config")).Sync(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if got := modelKeys(t, path); !slices.Equal(got, []string{"aaa/m1", "relay/glm-4.6"}) {
		t.Fatalf("reordered: %v, want aaa/m1 first\n%s", got, b)
	}
	if err := opencode(home, filepath.Join(home, ".config")).Sync(); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(path); !bytes.Equal(b, b2) {
		t.Fatalf("rewritten though in order:\n%s\n%s", b, b2)
	}
	if err := provider.SetModelOrder("opencode", nil); err != nil {
		t.Fatal(err)
	}
	if err := opencode(home, filepath.Join(home, ".config")).Sync(); err != nil {
		t.Fatal(err)
	}
	if got := modelKeys(t, path); !slices.Equal(got, want) {
		t.Fatalf("default: %v, want %v\n%s", got, want, readFile(path))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestOpenCodeWritesVariants: each model of uni-switch's in opencode.json names
// the reasoning levels it has as variants — OpenCode 2 otherwise offers low,
// medium and high for every one — and the rest of the entry is as before.
func TestOpenCodeWritesVariants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{
		ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "key",
		Models: []string{"deep", "wide", "plain"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "deep", Efforts: []string{"none", "high", "max"}, Context: 1000000, Output: 64000},
		{ID: "wide", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{ID: "plain"},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeFile(t, path, `{"theme":"dark"}`)
	if err := opencode(home, filepath.Join(home, ".config")).Field("model").Set("uni-switch/think/deep"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Theme    string `json:"theme"`
		Model    string `json:"model"`
		Provider map[string]struct {
			NPM    string                    `json:"npm"`
			Models map[string]map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dark" || cfg.Model != "uni-switch/think/deep" {
		t.Fatalf("config: %s", b)
	}
	p := cfg.Provider["uni-switch"]
	if p.NPM != "@ai-sdk/openai-compatible" {
		t.Fatalf("npm %q", p.NPM)
	}
	levels := func(id string) []string {
		t.Helper()
		m, ok := p.Models[id]
		if !ok {
			t.Fatalf("%s missing: %s", id, b)
		}
		vs, ok := m["variants"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no variants: %v", id, m)
		}
		var out []string
		for _, e := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
			if v, ok := vs[e]; ok {
				if !reflect.DeepEqual(v, map[string]any{"reasoningEffort": e}) {
					t.Fatalf("%s %s: %v", id, e, v)
				}
				out = append(out, e)
			}
		}
		if len(out) != len(vs) {
			t.Fatalf("%s: unexpected variants %v", id, vs)
		}
		return out
	}
	if got := levels("think/deep"); !reflect.DeepEqual(got, []string{"none", "high", "max"}) {
		t.Fatalf("deep: %v", got)
	}
	if got := levels("think/wide"); !reflect.DeepEqual(got, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("wide: %v", got)
	}
	if got := levels("think/plain"); got != nil {
		t.Fatalf("plain: %v", got)
	}
	if lim, _ := p.Models["think/deep"]["limit"].(map[string]any); lim["context"] != float64(1000000) {
		t.Fatalf("deep limit: %v", p.Models["think/deep"])
	}
}

// Xiaomi's mimo-v2.6-flash, which models.dev lists under Xiaomi with a
// thinking switch alone, gets no variants in opencode.json — under Xiaomi's
// preset or a relay of the user's alike — and no thinking levels in Pi:
// OpenCode offered it the resellers' levels up to max, which Xiaomi turns
// away with a 400 (#214). A model no maker lists keeps the resellers'. Pi is
// told it reasons, as /v1/models says (#2380), with no map, so Pi offers
// its own off to high and never xhigh or max.
func TestOpenCodeNoResellerLevelsForSwitchModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "xiaomi": {"models": {"mimo-v2.6-flash": {"id":"mimo-v2.6-flash","reasoning":true,"reasoning_options":[{"type":"toggle"}]}}},
	  "llmgateway": {"models": {
	    "deepinfra/mimo-v2.6-flash": {"id":"deepinfra/mimo-v2.6-flash","reasoning_options":[{"type":"effort","values":["none","minimal","low","medium","high","xhigh","max"]}]},
	    "xiaomi/mimo-v2.6-flash": {"id":"xiaomi/mimo-v2.6-flash","reasoning_options":[{"type":"effort","values":["none","minimal","low","medium","high","xhigh","max"]}]},
	    "open-model": {"id":"open-model","reasoning_options":[{"type":"effort","values":["low","high","max"]}]}}}
	}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	xiaomi, err := provider.FromPreset("xiaomi")
	if err != nil {
		t.Fatal(err)
	}
	xiaomi.Key, xiaomi.Models = "k", []string{"mimo-v2.6-flash"}
	if err := provider.Save(xiaomi); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "https://relay.test/v1", Key: "k",
		Models: []string{"mimo-v2.6-flash", "open-model", "mystery-model"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeFile(t, path, `{}`)
	if err := opencode(home, filepath.Join(home, ".config")).Field("model").Set("uni-switch/xiaomi/mimo-v2.6-flash"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Provider map[string]struct {
			Models map[string]struct {
				Variants map[string]any `json:"variants"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	ms := cfg.Provider["uni-switch"].Models
	for id, want := range map[string]int{"xiaomi/mimo-v2.6-flash": 0, "relay/mimo-v2.6-flash": 0, "relay/open-model": 3} {
		m, ok := ms[id]
		if !ok || m.Variants == nil || len(m.Variants) != want {
			t.Errorf("%s: variants %v (listed %v), want %d", id, m.Variants, ok, want)
		}
	}
	pi, _ := json.Marshal(uniSwitchProviderJSON("pi"))
	var piCfg struct {
		Models []struct {
			ID        string         `json:"id"`
			Reasoning bool           `json:"reasoning"`
			Levels    map[string]any `json:"thinkingLevelMap"`
		} `json:"models"`
	}
	json.Unmarshal(pi, &piCfg)
	seen := 0
	for _, m := range piCfg.Models {
		switch m.ID {
		case "xiaomi/mimo-v2.6-flash", "relay/mimo-v2.6-flash":
			seen++
			if !m.Reasoning || m.Levels != nil {
				t.Errorf("pi %s: reasoning %v, levels %v", m.ID, m.Reasoning, m.Levels)
			}
		case "relay/open-model":
			// the full map, levels the model lacks null so Pi hides them (#243)
			seen++
			want := map[string]any{"off": nil, "minimal": nil, "low": "low", "medium": nil, "high": "high", "xhigh": nil, "max": "max"}
			if !m.Reasoning || !reflect.DeepEqual(m.Levels, want) {
				t.Errorf("pi %s: reasoning %v, levels %v", m.ID, m.Reasoning, m.Levels)
			}
		case "relay/mystery-model":
			// levels uni-switch doesn't know: Pi's own, as before
			seen++
			if m.Reasoning || m.Levels != nil {
				t.Errorf("pi %s: reasoning %v, levels %v", m.ID, m.Reasoning, m.Levels)
			}
		}
	}
	if seen != 4 {
		t.Errorf("pi models: %s", pi)
	}
}

// TestOpenCodeZenModelReasons: an OpenCode Zen model that thinks with no
// levels to pick (mimo-v2.6-flash-free, models.dev: reasoning true, no
// reasoning_options) is marked reasoning in opencode.json, which OpenCode's
// model tooltip showed as 不支持推理 through uni-switch (#725); so is one whose
// levels hold OpenCode's own low, medium and high. One with fewer levels
// isn't (OpenCode would add the levels it lacks), nor one that doesn't think.
func TestOpenCodeZenModelReasons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "opencode": {"models": {
	    "mimo-v2.6-flash-free": {"id":"mimo-v2.6-flash-free","reasoning":true,"reasoning_options":[]},
	    "deepseek-v4-flash-free": {"id":"deepseek-v4-flash-free","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high","max"]}]},
	    "laguna-s-2.1-free": {"id":"laguna-s-2.1-free","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","medium","high"]}]},
	    "ling-2.6-flash-free": {"id":"ling-2.6-flash-free","reasoning":false}}}
	}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	zen, err := provider.FromPreset("opencode-zen")
	if err != nil {
		t.Fatal(err)
	}
	zen.Models = []string{"mimo-v2.6-flash-free", "deepseek-v4-flash-free", "laguna-s-2.1-free", "ling-2.6-flash-free"}
	if err := provider.Save(zen); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeFile(t, path, `{}`)
	if err := opencode(home, filepath.Join(home, ".config")).Field("model").Set("uni-switch/opencode-zen/mimo-v2.6-flash-free"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Provider map[string]struct {
			Models map[string]struct {
				Reasoning *bool          `json:"reasoning"`
				Variants  map[string]any `json:"variants"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	ms := cfg.Provider["uni-switch"].Models
	for id, want := range map[string]bool{"mimo-v2.6-flash-free": true, "laguna-s-2.1-free": true,
		"deepseek-v4-flash-free": false, "ling-2.6-flash-free": false} {
		m, ok := ms["opencode-zen/"+id]
		if !ok {
			t.Errorf("%s missing: %s", id, b)
			continue
		}
		if got := m.Reasoning != nil && *m.Reasoning; got != want {
			t.Errorf("%s: reasoning %v, want %v (variants %v)", id, got, want, m.Variants)
		}
	}
}
