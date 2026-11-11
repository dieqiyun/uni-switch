package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch's own files written while a sync's filter is on, with ~/.config a
// link into another folder (a dotfiles repo; ylorn on Discord): the window's
// size saved after a resize (settings.Save, under the settings' lock) and
// wsl.json (under wsl's) are written, the filter leaves them as they are,
// and nothing waits on the lock its own writer holds. The GUI hung for
// good ~10s after every start, with SIGTERM ignored.
func TestOwnWritesDuringASyncWithALinkedConfig(t *testing.T) {
	home := syncHome(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	cfg := filepath.Join(home, ".config")
	if err := os.RemoveAll(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), cfg); err != nil {
		t.Skip(err)
	}
	s := settings.Load()
	s.Port = 3593
	// an address on another machine and keys in settings.json: what the
	// filter would weigh, with the LAN key, were it not uni-switch's own
	s.UpdateMirror = "https://mirror.example/"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	within(t, "settings.Save", func() error {
		return synced(func() error {
			s := settings.Load()
			s.Window = []int{1200, 800}
			return settings.Save(s)
		})
	})
	if got := settings.Load(); len(got.Window) != 2 || got.Window[0] != 1200 || got.UpdateMirror != "https://mirror.example/" {
		t.Errorf("settings as written: window %v, mirror %q", got.Window, got.UpdateMirror)
	}

	// wsl.json, written under the lock wslHost takes, holding an address
	// the filter would ask wslHost about
	wsl.Lock()
	was := wsl.seen
	wsl.Unlock()
	t.Cleanup(func() {
		wsl.Lock()
		wsl.seen, wsl.dirty = was, false
		wsl.Unlock()
	})
	seen := map[string]*distro{"Ubuntu": {Name: "Ubuntu", Gateway: "172.30.0.1", Values: map[string]string{"base": "http://192.168.1.20:3425/v1"}}}
	b, _ := json.Marshal(seen)
	if err := os.WriteFile(wslStatePath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	seen["Ubuntu"].Values["base"] = "http://127.0.0.1:3593/v1"
	within(t, "wsl.json", func() error {
		return synced(func() error {
			wsl.Lock()
			defer wsl.Unlock()
			wsl.seen, wsl.dirty = seen, true
			wslSaveLocked()
			return nil
		})
	})
	if b, _ := os.ReadFile(wslStatePath()); !json.Valid(b) || !strings.Contains(string(b), "127.0.0.1:3593") {
		t.Errorf("wsl.json as written: %s", b)
	}
}

// within runs fn and fails the run when it hasn't returned in 10s: a
// goroutine stuck on a lock it holds can't be ended, so the binary is.
func within(t *testing.T, what string, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(10 * time.Second):
		// not a panic: its cleanups would wait on the lock too
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		fmt.Fprintf(os.Stderr, "--- FAIL: %s: %s deadlocked under a sync's filter:\n%s\n", t.Name(), what, buf)
		os.Exit(1)
	}
}
