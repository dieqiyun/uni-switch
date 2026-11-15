//go:build !windows

package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// fifoWatch turns each path into a FIFO and notes every open of one for
// reading: a non-blocking open for writing succeeds only while a reader
// waits in its open, which is handed "{}" (once more for each loop that
// finds it still open) and an end of file. The func returned waits for a
// whole loop that began after the call, so an open the loop found before
// then is in what it returns.
func fifoWatch(t *testing.T, paths []string) func() []string {
	t.Helper()
	for _, p := range paths {
		os.Remove(p)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var (
		mu     sync.Mutex
		opened []string
		loops  int
	)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, p := range paths {
				fd, err := syscall.Open(p, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
				if err != nil {
					continue
				}
				mu.Lock()
				opened = append(opened, p)
				mu.Unlock()
				_, _ = syscall.Write(fd, []byte("{}"))
				_ = syscall.Close(fd)
			}
			mu.Lock()
			loops++
			mu.Unlock()
			time.Sleep(200 * time.Microsecond)
		}
	}()
	// a FIFO left behind blocks the next reader for good
	t.Cleanup(func() {
		close(stop)
		<-done
		for _, p := range paths {
			os.Remove(p)
		}
	})
	return func() []string {
		mu.Lock()
		// the loop under way may have opened a FIFO and not noted it yet;
		// the one after it begins after this call
		for until := loops + 2; loops < until; {
			mu.Unlock()
			time.Sleep(100 * time.Microsecond)
			mu.Lock()
		}
		defer mu.Unlock()
		return slices.Clone(opened)
	}
}

// With account discovery off (#1535, MaxHaiCom), the Agents page's "runs
// on" line, OpenCode's own providers and wiring Codex read no agent's
// sign-in: not Claude Code's account in .claude.json, not Codex's
// auth.json, not OpenCode's auth.json.
func TestAccountDiscoveryOffAgentsReadNoSignIn(t *testing.T) {
	home, _ := codexHome(t, "", "")
	if err := settings.Save(settings.Settings{NoAccountDiscovery: true}); err != nil {
		t.Fatal(err)
	}
	claudeDir := filepath.Join(home, ".claude")
	ocAuth := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	files := []string{
		filepath.Join(home, ".codex", "auth.json"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(claudeDir, ".claude.json"),
		ocAuth,
	}
	opened := fifoWatch(t, files)
	// a reader slow to reach the end is handed "{}" again by the next
	// loop's write, and noted again, so under load it reads "{}{}"
	if b, err := os.ReadFile(files[0]); err != nil || len(b) == 0 || strings.ReplaceAll(string(b), "{}", "") != "" {
		t.Fatalf("the watch didn't answer a read: %q %v", b, err)
	}
	checked := len(opened())

	cfgDir := filepath.Join(home, ".config", "opencode")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "opencode.json"), []byte(`{"provider":{"mine":{}}}`), 0o644)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{}`), 0o644)

	if s := (&Agent{ID: "claude", Path: filepath.Join(claudeDir, "settings.json")}).Source(); s != "" {
		t.Errorf("Claude Code runs on %q", s)
	}
	if s := (&Agent{ID: "codex", Path: filepath.Join(home, ".codex", "config.toml")}).Source(); s != "" {
		t.Errorf("Codex runs on %q", s)
	}
	// its own config still counts
	if s := (&Agent{ID: "opencode", Path: filepath.Join(cfgDir, "opencode.json")}).Source(); s != "providers:1" {
		t.Errorf("OpenCode runs on %q, want its config's one provider", s)
	}
	ownOptions(ocAuth, "")
	cx := codex(home)
	if _, err := cx.Join(); err != nil {
		t.Fatal(err)
	}
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if got := opened()[checked:]; len(got) != 0 {
		t.Errorf("with discovery off, reads of sign-ins: %s", strings.Join(got, " "))
	}
}
