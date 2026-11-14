package rotation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func manifest(t *testing.T) pluginrt.Manifest {
	t.Helper()
	for _, m := range pluginrt.Registered() {
		if m.ID == "rotation" {
			return m
		}
	}
	t.Fatal("rotation not registered")
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

// On, every routing besides Smart is served; off, none is, and Smart still
// is; on again, all are back.
func TestOnServesOffDoesNot(t *testing.T) {
	sandbox(t)
	rt := pluginrt.New([]pluginrt.Manifest{manifest(t)})
	defer rt.Stop()
	served := func(want bool) {
		t.Helper()
		for name := range routes.Builtin {
			if routes.Served(name) != want {
				t.Fatalf("%s served: %v, want %v (%+v)", name, !want, want, rt.Statuses())
			}
		}
		if !routes.Served("") {
			t.Fatal("Smart isn't served")
		}
	}
	served(false)
	rt.Reconcile(map[string]bool{"rotation": true}, nil)
	served(true)
	rt.Reconcile(map[string]bool{"rotation": false}, nil)
	served(false)
	rt.Reconcile(map[string]bool{"rotation": true}, nil)
	served(true)
}

// In use while a provider is routed by one of its routings, by
// providers.json alone; Smart and Manual don't count, and a providers.json
// that can't be read keeps it on.
func TestInUse(t *testing.T) {
	sandbox(t)
	m := manifest(t)
	if n, err := m.InUse(); err != nil || n != 0 {
		t.Fatalf("no providers: %d %v", n, err)
	}
	for _, id := range []string{"a", "b"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "k", Models: []string{"m"}, Chat: "http://127.0.0.1:1/v1"}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := m.InUse(); err != nil || n != 0 {
		t.Fatalf("Smart and Manual: %d %v", n, err)
	}
	if err := provider.SetRouting("a", provider.Rotate); err != nil {
		t.Fatal(err)
	}
	if n, err := m.InUse(); err != nil || n != 1 {
		t.Fatalf("a provider in turn: %d %v", n, err)
	}
	if err := os.WriteFile(provider.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bak := range []string{".bak", ".lastgood"} {
		os.Remove(provider.Path() + bak)
	}
	if _, err := m.InUse(); err == nil {
		t.Fatal("an unreadable providers.json counted as known")
	}
}
