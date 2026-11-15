package search

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gwtool"
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func manifest(t *testing.T) pluginrt.Manifest {
	t.Helper()
	for _, m := range pluginrt.Registered() {
		if m.ID == "search" {
			return m
		}
	}
	t.Fatal("search not registered")
	return pluginrt.Manifest{}
}

func sandbox(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
}

// In a process serving the gateway, on serves uni-switch's web search, off
// doesn't, on again does; a process not serving it has it either way.
func TestOnServesOffDoesNot(t *testing.T) {
	sandbox(t)
	if !gwtool.Served(gwtool.WebSearch) {
		t.Fatal("not served outside the gateway")
	}
	stop := gwtool.Gate()
	defer stop()
	rt := pluginrt.New([]pluginrt.Manifest{manifest(t)})
	defer rt.Stop()
	served := func(want bool) {
		t.Helper()
		if ok := gwtool.Served(gwtool.WebSearch); ok != want {
			t.Fatalf("search served: %v, want %v (%+v)", ok, want, rt.Statuses())
		}
	}
	served(false)
	rt.Reconcile(map[string]bool{"search": true}, nil)
	served(true)
	rt.Reconcile(map[string]bool{"search": false}, nil)
	served(false)
	rt.Reconcile(map[string]bool{"search": true}, nil)
	served(true)
}

// In use while a search API is set up or a searcher is picked; uni-switch's
// pick or off: none; files that can't be read: not known.
func TestInUse(t *testing.T) {
	sandbox(t)
	count := func(want int) {
		t.Helper()
		if n, err := InUse(); err != nil || n != want {
			t.Fatalf("in use %d %v, want %d", n, err, want)
		}
	}
	count(0)
	for _, s := range []string{"", "off"} {
		if err := settings.Save(settings.Settings{Searcher: s}); err != nil {
			t.Fatal(err)
		}
		count(0)
	}
	if err := settings.Save(settings.Settings{Searcher: "openai/gpt-5.4-mini"}); err != nil {
		t.Fatal(err)
	}
	count(1)
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "tavily", Key: "tvly-k"}); err != nil {
		t.Fatal(err)
	}
	count(2)
	if err := os.WriteFile(settings.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bak := range []string{".bak", ".lastgood"} {
		os.Remove(settings.Path() + bak)
	}
	if _, err := InUse(); err == nil {
		t.Fatal("unreadable settings counted as known")
	}
	if err := os.Remove(settings.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bak := range []string{".bak", ".lastgood"} {
		os.Remove(provider.Path() + bak)
	}
	if _, err := InUse(); err == nil {
		t.Fatal("unreadable providers.json counted as known")
	}
}
