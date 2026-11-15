package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// #2157 (sstdgm): a custom provider's nyro-gpt-5.5 is GPT-5.5 on the
// relay's channel, and Codex's catalog says it takes images; its
// nyro-deepseek-v4.1-flash, text-only by models.dev, stays text-only, and
// the user's own word on a model it doesn't know still counts.
func TestChannelModelsReachCodexWithImages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{
	  "openai":{"models":{"gpt-5.5":{"id":"gpt-5.5","modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":400000,"output":128000}}}},
	  "deepseek":{"models":{"deepseek-v4.1-flash":{"id":"deepseek-v4.1-flash","modalities":{"input":["text"],"output":["text"]},"limit":{"context":1000000,"output":65536}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	p := Provider{ID: "litelln-nyro"}
	// the user said the channel's own nyro-gpt-5.5-tj, which models.dev
	// doesn't know, takes images
	s := settings.Settings{ModelImages: map[string]bool{"litelln-nyro/nyro-gpt-5.5-tj": true}}
	var ms []catalog.Model
	for _, id := range []string{"nyro-gpt-5.5", "nyro-deepseek-v4.1-flash", "nyro-gpt-5.5-tj"} {
		e := entryFor(p, catalog.Model{ID: id}, s)
		ms = append(ms, catalog.Model{ID: e.ID, Name: e.Name, Images: e.Images})
	}
	var cat struct {
		Models []struct {
			Slug       string   `json:"slug"`
			Modalities []string `json:"input_modalities"`
		} `json:"models"`
	}
	if err := json.Unmarshal(codexcat.CatalogAt(ms, ""), &cat); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"litelln-nyro/nyro-gpt-5.5": true, "litelln-nyro/nyro-deepseek-v4.1-flash": false, "litelln-nyro/nyro-gpt-5.5-tj": true}
	for _, m := range cat.Models {
		if got := slices.Contains(m.Modalities, "image"); got != want[m.Slug] {
			t.Errorf("%s: input_modalities %v, want image %v", m.Slug, m.Modalities, want[m.Slug])
		}
	}
	if len(cat.Models) != 3 {
		t.Fatalf("catalog has %d models", len(cat.Models))
	}
}
