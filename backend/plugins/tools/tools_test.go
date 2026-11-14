package tools

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gwtool"

	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func manifest(t *testing.T) pluginrt.Manifest {
	t.Helper()
	for _, m := range pluginrt.Registered() {
		if m.ID == "tools" {
			return m
		}
	}
	t.Fatal("tools not registered")
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

// In a process serving the gateway, on serves the plugins' tools, off
// doesn't, on again does; a process not serving it has them either way.
func TestOnServesOffDoesNot(t *testing.T) {
	sandbox(t)
	if !gwtool.Served(gwtool.PluginTools) {
		t.Fatal("not served outside the gateway")
	}
	stop := gwtool.Gate()
	defer stop()
	rt := pluginrt.New([]pluginrt.Manifest{manifest(t)})
	defer rt.Stop()
	served := func(want bool) {
		t.Helper()
		if ok := gwtool.Served(gwtool.PluginTools); ok != want {
			t.Fatalf("tools served: %v, want %v (%+v)", ok, want, rt.Statuses())
		}
	}
	served(false)
	rt.Reconcile(map[string]bool{"tools": true}, nil)
	served(true)
	rt.Reconcile(map[string]bool{"tools": false}, nil)
	served(false)
	rt.Reconcile(map[string]bool{"tools": true}, nil)
	served(true)
}
