package budget

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/gwbudget"
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func manifest(t *testing.T) pluginrt.Manifest {
	t.Helper()
	for _, m := range pluginrt.Registered() {
		if m.ID == "budget" {
			return m
		}
	}
	t.Fatal("budget not registered")
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

// On serves the limiter, off doesn't, and a key held to a limit is then
// refused rather than let in unlimited; on again serves it.
func TestOnServesOffDoesNot(t *testing.T) {
	sandbox(t)
	who := access.Identity{KeyID: "k1", KeyName: "Capped", Limit: &access.Limit{Period: "day", Tokens: 100}}
	rt := pluginrt.New([]pluginrt.Manifest{manifest(t)})
	defer rt.Stop()
	limited := func(want bool) {
		t.Helper()
		l, err := gwbudget.Limiting(who)
		if want && (l == nil || err != nil) {
			t.Fatalf("on: limiter %v, %v (%+v)", l, err, rt.Statuses())
		}
		if !want && (l != nil || !errors.Is(err, gwbudget.ErrUnlimited)) {
			t.Fatalf("off: limiter %v, %v, not refused (%+v)", l, err, rt.Statuses())
		}
	}
	limited(false)
	rt.Reconcile(map[string]bool{"budget": true}, nil)
	limited(true)
	rt.Reconcile(map[string]bool{"budget": false}, nil)
	limited(false)
	rt.Reconcile(map[string]bool{"budget": true}, nil)
	limited(true)
	if l, err := gwbudget.Limiting(access.Identity{KeyID: "k2", KeyName: "Free"}); l != nil || err != nil {
		t.Errorf("a key with no limit asked the limiter: %v, %v", l, err)
	}
}

// In use while a key has a limit; a key without one doesn't count, and
// caller-keys.json that can't be read is an error, so it stays on.
func TestInUse(t *testing.T) {
	sandbox(t)
	count := func(want int) {
		t.Helper()
		if n, err := InUse(); err != nil || n != want {
			t.Fatalf("InUse = %d, %v; want %d", n, err, want)
		}
	}
	count(0)
	for _, name := range []string{"Capped", "Free"} {
		if _, err := access.Update("add-key", access.Change{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	count(0)
	keys, err := access.List()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("limit-key", access.Change{Key: keys[0].ID, Limit: &access.Limit{Period: "day", Tokens: 700}}); err != nil {
		t.Fatal(err)
	}
	count(1)
	if err := os.WriteFile(access.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InUse(); err == nil {
		t.Error("an unreadable caller-keys.json counted as known")
	}
}
