package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every command ran start-up's moves over All(), which lists WSL's
// distros: on a Windows whose wsl.exe hangs, `uni-switch version` took 20s,
// two listings' timeouts. The moves look at this machine's agents only.
func TestStartUpMovesAskNoWSL(t *testing.T) {
	syncHome(t)
	t.Cleanup(FakeWSL(map[string]string{"Ubuntu": "home:/home/me\ndir:.codex\n"}, map[string]string{"Ubuntu": t.TempDir()}))
	var asked []string
	run := wslRun
	wslRun = func(d time.Duration, args ...string) ([]byte, error) {
		asked = append(asked, strings.Join(args, " "))
		return run(d, args...)
	}
	RenameLegacy()
	MoveCursorEfforts()
	MoveAntigravityEfforts()
	if len(asked) != 0 {
		t.Fatalf("start-up asked wsl.exe %q", asked)
	}
	// All() still has the distro's agents
	n := 0
	for _, a := range All() {
		if a.WSL != "" {
			n++
		}
	}
	if n == 0 || len(asked) == 0 {
		t.Fatalf("All() found %d WSL agents, asked %q", n, asked)
	}
}

// A wsl.exe that can't list the distros isn't asked which of them run:
// that is a second timeout, under the lock the GUI waits on.
func TestWSLListFailsAsksNoRunning(t *testing.T) {
	fakeWSL(t, "", "", nil, nil)
	var asked []string
	wslRun = func(_ time.Duration, args ...string) ([]byte, error) {
		asked = append(asked, strings.Join(args, " "))
		return nil, errors.New("timed out")
	}
	wslDistros()
	if len(asked) != 1 || asked[0] != "-l -q" {
		t.Fatalf("asked %q", asked)
	}
	// and not again on the next look within the failed age
	wslUp("Ubuntu")
	if len(asked) != 1 {
		t.Fatalf("asked %q", asked)
	}
}

// A wsl.exe that hung to its timeout is remembered beside uni-switch's
// settings: the next command (a new process) doesn't wait on it again,
// and once a listing answers, it is forgotten.
func TestWSLHangRememberedAcrossProcesses(t *testing.T) {
	fakeWSL(t, "", "", nil, nil)
	after := wslHungAfter
	wslHungAfter = 0
	t.Cleanup(func() { wslHungAfter = after; os.Remove(wslHungPath()) })
	os.MkdirAll(filepath.Dir(wslHungPath()), 0o755)
	asks := 0
	hung := true
	wslRun = func(_ time.Duration, args ...string) ([]byte, error) {
		asks++
		if hung {
			return nil, errors.New("timed out")
		}
		return []byte("Ubuntu\n"), nil
	}
	newProcess := func() {
		wsl.Lock()
		wsl.at, wsl.runAt, wsl.names, wsl.running = time.Time{}, time.Time{}, nil, nil
		wsl.Unlock()
	}
	wslDistros()
	if asks != 1 || !wslHungLately() {
		t.Fatalf("asks %d, remembered %v", asks, wslHungLately())
	}
	newProcess()
	wslDistros()
	if asks != 1 {
		t.Fatalf("a new process asked a hung wsl.exe again: %d", asks)
	}
	// it is asked again once the mark is old, and an answer clears it
	old := time.Now().Add(-wslHungAge - time.Minute)
	if err := os.Chtimes(wslHungPath(), old, old); err != nil {
		t.Fatal(err)
	}
	hung = false
	newProcess()
	wslDistros()
	if asks < 2 || wslHungLately() {
		t.Fatalf("asks %d, remembered %v", asks, wslHungLately())
	}
	if _, err := os.Stat(wslHungPath()); !os.IsNotExist(err) {
		t.Fatalf("mark kept: %v", err)
	}
}
