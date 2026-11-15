package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"gopkg.in/yaml.v3"
)

func TestConfiguredModelsAdvertiseImageInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{
		ID: "vision", Name: "Vision", Chat: "https://example.test/v1", Key: "key",
		Models: []string{"image", "text", "unknown"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("vision", "https://example.test/v1", []catalog.Model{
		{ID: "image", Images: true, ImageInput: imageInputBool(true)},
		{ID: "text", ImageInput: imageInputBool(false)},
		{ID: "unknown"},
	}); err != nil {
		t.Fatal(err)
	}
	read := func(path string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	// a config uni-switch wrote before #2209 has attachment on its models;
	// the next write takes it away
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(ocPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ocPath, []byte(`{"provider":{"uni-switch":{"models":{"vision/image":{"name":"image","attachment":true}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := opencode(home, filepath.Join(home, ".config")).Field("model").Set("uni-switch/vision/image"); err != nil {
		t.Fatal(err)
	}
	oc := read(filepath.Join(home, ".config", "opencode", "opencode.json"))
	ocModels := oc["provider"].(map[string]any)["uni-switch"].(map[string]any)["models"].(map[string]any)
	image := ocModels["vision/image"].(map[string]any)
	// OpenCode 2 warns on attachment, a V1 field it ignores (#2209)
	if _, ok := image["attachment"]; ok {
		t.Fatalf("OpenCode image model still has attachment: %v", image)
	}
	modalities := image["modalities"].(map[string]any)
	if !reflect.DeepEqual(modalities["input"], []any{"text", "image"}) || !reflect.DeepEqual(modalities["output"], []any{"text"}) {
		t.Fatalf("OpenCode image modalities: %v", modalities)
	}
	for _, id := range []string{"vision/text", "vision/unknown"} {
		entry := ocModels[id].(map[string]any)
		if entry["attachment"] != nil || entry["modalities"] != nil {
			t.Fatalf("OpenCode %s incorrectly advertises image input: %v", id, entry)
		}
	}

	if err := pi(home).Field("model").Set("uni-switch/vision/image"); err != nil {
		t.Fatal(err)
	}
	pm := read(filepath.Join(home, ".pi", "agent", "models.json"))
	piModels := pm["providers"].(map[string]any)["uni-switch"].(map[string]any)["models"].([]any)
	if len(piModels) != 3 {
		t.Fatalf("Pi models: %v", piModels)
	}
	for _, raw := range piModels {
		entry := raw.(map[string]any)
		switch entry["id"] {
		case "vision/image":
			if !reflect.DeepEqual(entry["input"], []any{"text", "image"}) {
				t.Fatalf("Pi image input: %v", entry)
			}
		case "vision/text", "vision/unknown":
			if entry["input"] != nil {
				t.Fatalf("Pi %s incorrectly advertises image input: %v", entry["id"], entry)
			}
		default:
			t.Fatalf("unexpected Pi model: %v", entry)
		}
	}

	// omp: an entry written before input was, brought up to date by Sync
	ompModels := filepath.Join(home, ".omp", "agent", "models.yml")
	os.MkdirAll(filepath.Dir(ompModels), 0o755)
	os.WriteFile(ompModels, []byte("providers:\n  uni-switch:\n    baseUrl: x\n    models:\n      - id: vision/image\n"), 0o644)
	if err := omp(home).Sync(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ompModels)
	var om struct {
		Providers map[string]struct {
			Models []struct {
				ID    string
				Input []string
			}
		}
	}
	if err := yaml.Unmarshal(b, &om); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"vision/image": {"text", "image"}, "vision/text": {"text"}, "vision/unknown": nil}
	got := map[string][]string{}
	for _, m := range om.Providers["uni-switch"].Models {
		got[m.ID] = m.Input
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("omp input: %v\n%s", got, b)
	}
}

func imageInputBool(v bool) *bool { return &v }

func TestAPIKeyGroupImagesReachAgentCatalogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"anthropic":{"models":{"claude-sonnet-4-5":{"id":"claude-sonnet-4-5","modalities":{"input":["text","image"]}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for _, p := range []provider.Provider{
		{ID: "confirmed", Catalog: "anthropic", Chat: "https://example.test/v1", Key: "key", KeyName: "Vision", KeyGroups: true, Models: []string{"claude-sonnet-4-5"}},
		{ID: "unknown", Chat: "https://example.test/v1", Key: "key", Models: []string{"claude-sonnet-4-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.SaveLive("unknown", "https://example.test/v1", []catalog.Model{{ID: "claude-sonnet-4-5"}}); err != nil {
		t.Fatal(err)
	}
	id := "confirmed/~key/" + provider.KeyID("key") + "/claude-sonnet-4-5"
	ms := uniSwitchProviderJSON("opencode").(map[string]any)["models"].(orderedJSON)
	idx := slices.IndexFunc(ms, func(p jsonPair) bool { return p.k == id })
	if idx < 0 {
		t.Fatalf("OpenCode key group missing: %s", id)
	}
	oc, _ := ms[idx].v.(map[string]any)
	if _, ok := oc["attachment"]; ok || !reflect.DeepEqual(oc["modalities"].(map[string]any)["input"], []string{"text", "image"}) {
		t.Fatalf("OpenCode group image input: %v", oc)
	}
	pi := uniSwitchProviderJSON("pi").(map[string]any)["models"].([]map[string]any)
	found := false
	for _, m := range pi {
		if m["id"] == id {
			found = true
			if !reflect.DeepEqual(m["input"], []string{"text", "image"}) {
				t.Fatalf("Pi group image input: %v", m)
			}
		}
	}
	if !found {
		t.Fatal("Pi group missing")
	}
	var codex struct {
		Models []struct {
			Slug  string   `json:"slug"`
			Input []string `json:"input_modalities"`
		} `json:"models"`
	}
	if err := json.Unmarshal(codexcat.Catalog(uniSwitchModels("codex")), &codex); err != nil {
		t.Fatal(err)
	}
	for _, m := range codex.Models {
		if m.Slug == id {
			if !reflect.DeepEqual(m.Input, []string{"text", "image"}) {
				t.Fatalf("Codex group image input: %v", m.Input)
			}
			return
		}
	}
	t.Fatal("Codex group missing")
}
