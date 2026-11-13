package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ARNO on Discord: a model's output limit wasn't in the Gateway's model
// tooltip, though agents were told one when it was written to their
// config. The page's models carried no output at all, so the tooltip had
// a line only for routing groups. Each model now says the reply limit
// agents are told: the vendor's list's, else models.dev's, else the
// user's Max output over both.
func TestProviderModelOutputSaid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "zai":{"id":"zai","models":{"glm-5.3-flash":{"id":"glm-5.3-flash","limit":{"context":1000000,"output":131072}}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"zai/glm-5.3-flash", "own-model"}}); err != nil {
		t.Fatal(err)
	}
	said := func() map[string]int {
		t.Helper()
		p, err := provider.Find("relay")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		for _, m := range providerInfo(*p, nil).Models {
			out[m.ID] = m.Output
		}
		for _, e := range provider.Served() {
			if n, ok := out[e.Model]; ok && n != e.Output {
				t.Errorf("%s: the page says %d, agents are told %d", e.Model, n, e.Output)
			}
		}
		return out
	}
	if got := said(); got["zai/glm-5.3-flash"] != 131072 || got["own-model"] != 0 {
		t.Fatalf("models.dev's: %v", got)
	}
	if err := provider.SetModelOutput("relay/own-model", 64_000); err != nil {
		t.Fatal(err)
	}
	if got := said(); got["own-model"] != 64_000 {
		t.Fatalf("the user's: %v", got)
	}
}
