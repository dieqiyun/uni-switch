package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// #2344: the model editor greys in the price a model is costed at. A
// model no list prices, with a Same as, is costed at the maker's price of
// the model it is the same as, so the editor shows that one, beside the
// Same as box that says why; one with none still shows none.
func TestModelEditorShowsTheSameAsPrice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "moonshotai":{"id":"moonshotai","models":{"kimi-k3":{"id":"kimi-k3","cost":{"input":0.6,"output":2.5}}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"kimi-k3-1", "other"}}); err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	s.ModelSameAs = map[string]string{"relay/kimi-k3-1": "kimi-k3"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range providerInfo(*p, nil).Models {
		switch m.ID {
		case "kimi-k3-1":
			if m.List == nil || m.List.Input != 0.6 || m.Same != "kimi-k3" {
				t.Errorf("kimi-k3-1: list %+v, same %q; want Kimi K3's $0.6", m.List, m.Same)
			}
		case "other":
			if m.List != nil {
				t.Errorf("a model with no Same as shows a list price: %+v", m.List)
			}
		}
	}
}
