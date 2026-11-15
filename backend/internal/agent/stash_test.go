package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func stashHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

// Every agent's originals are in one stash.json, and each agent's connect
// and disconnect read, change and write it: two at once (the GUI's Connect
// all, a gateway move setting every agent again) must keep both changes.
func TestStashKeepsChangesMadeAtOnce(t *testing.T) {
	stashHome(t)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stash(map[string]string{fmt.Sprintf("agent%d.was", i): "theirs"})
		}()
	}
	wg.Wait()
	if n := len(stashLoad()); n != 40 {
		t.Fatalf("%d of 40 agents' originals kept", n)
	}
}

// A stash.json that doesn't parse is read as no stash, and the next change
// writes over it: what it held, every agent's originals, is copied aside
// first, so it can still be got back.
func TestUnreadStashKeptAside(t *testing.T) {
	stashHome(t)
	p := stashPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	half := `{"claude.was": "sk-their-own", "codex.wa`
	if err := os.WriteFile(p, []byte(half), 0o600); err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{"pi.was": "x"})
	if got := stashLoad(); got["pi.was"] != "x" {
		t.Fatalf("stash after the change: %v", got)
	}
	bad, _ := filepath.Glob(p + ".bad-*")
	if len(bad) != 1 {
		t.Fatalf("kept aside: %v", bad)
	}
	if b, _ := os.ReadFile(bad[0]); string(b) != half {
		t.Fatalf("kept aside as %q", b)
	}
	// a stash that parses is written over as before, nothing more kept
	forget("pi.was")
	if more, _ := filepath.Glob(p + ".bad-*"); len(more) != 1 {
		t.Fatalf("kept aside after a good write: %v", more)
	}
	if fi, err := os.Stat(p); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("stash.json is %v; it holds keys", fi.Mode().Perm())
	}
}

// stashLoad reads a stash.json that is valid JSON but not the stash's map
// (an array) as empty too: it is kept aside like one cut short, not taken
// as a stash because json.Valid passes it (#2263).
func TestUnreadStashArrayKeptAside(t *testing.T) {
	stashHome(t)
	p := stashPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{"pi.was": "x"})
	bad, _ := filepath.Glob(p + ".bad-*")
	if len(bad) != 1 {
		t.Fatalf("kept aside: %v", bad)
	}
	if b, _ := os.ReadFile(bad[0]); string(b) != `[]` {
		t.Fatalf("kept aside as %q", b)
	}
}
