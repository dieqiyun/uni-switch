package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// lml on Discord (Windows, about ten providers): the Providers page took
// seconds to come up. Each model's row read the settings three times over
// (its image answer twice, its reply limit once), parsing settings.json
// again each time even while the request held it: with the owner's 23
// providers that was most of /api/providers (~0.5s on Windows). A
// provider's list now reads them once, and still says what they hold.
//
// The parses are counted, not timed: a 2s bar on the call failed on a
// loaded machine with the settings read once (#2185).
func TestProviderListReadsSettingsOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var models []string
	for i := range 300 {
		models = append(models, fmt.Sprintf("model-%d", i))
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	// image answers of other providers' models too, as a long-used
	// uni-switch's settings hold
	images := map[string]bool{"relay/model-7": true}
	for i := range 200 {
		images[fmt.Sprintf("other-%d/some-model-%d", i%50, i)] = i%2 == 0
	}
	raw, err := json.Marshal(map[string]any{
		"modelImages":  images,
		"modelOutputs": map[string]int{"relay/model-9": 64000},
	})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(settings.Path()), 0o755)
	if err := os.WriteFile(settings.Path(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	// as a GET /api/* is served (held)
	defer provider.Hold()()
	defer filememo.Hold()()
	providerInfo(*p, nil) // the first read of each file
	before := settings.Parses()
	info := providerInfo(*p, nil)
	// a few for the list (6 here); read for each model it is once to
	// three times per model (300 to 900). The bound sits far from both,
	// leaving room for a read some other look makes meanwhile.
	n := settings.Parses() - before
	t.Logf("settings.json parsed %d times", n)
	if n >= 50 {
		t.Errorf("one provider's %d models parsed settings.json %d times: the settings were read for each", len(models), n)
	}
	byID := map[string]modelJSON{}
	for _, m := range info.Models {
		byID[m.ID] = m
	}
	if m := byID["model-7"]; !m.Images || !m.ImageSet {
		t.Errorf("the user's image answer is lost: %+v", m)
	}
	if m := byID["model-8"]; m.ImageSet {
		t.Errorf("an image answer nobody gave: %+v", m)
	}
	if m := byID["model-9"]; m.Output != 64000 {
		t.Errorf("the user's reply limit is lost: %+v", m)
	}
}
