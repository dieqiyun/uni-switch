package provider

import (
	"bufio"
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// The Restart on the Codex app's "still signed in as" notice (nb5p_hQ on
// X) ends the app's app-servers now, a running turn or not, waits for
// the app to start another, and says so when it doesn't: none running,
// one that can't be ended or stays, none back, a listing that fails.
func TestRestartSwitchedCodexApp(t *testing.T) {
	s := stubCodexApp(t)
	oldGone, oldBack := codexAppGoneWait, codexAppBackWait
	codexAppGoneWait, codexAppBackWait = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() {
		codexAppGoneWait, codexAppBackWait = oldGone, oldBack
		DismissCodexApp()
	})
	ctx := context.Background()

	if err := RestartSwitchedCodexApp(ctx); !errors.Is(err, ErrCodexAppGone) {
		t.Fatalf("no app running: %v", err)
	}

	codexDaemonMu.Lock()
	codexAppLeft = leftDaemon{pid: 7, user: "spent@x"}
	codexDaemonMu.Unlock()
	s.running, s.turnOpen = []int{7, 8}, true
	if err := RestartSwitchedCodexApp(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.ended, []int{7, 8}) {
		t.Fatalf("ended %v, want both of the app's app-servers, a turn open or not (the user said yes)", s.ended)
	}
	codexDaemonMu.Lock()
	left := codexAppLeft
	codexDaemonMu.Unlock()
	if left.user != "" {
		t.Fatalf("still said to be on %q after its restart", left.user)
	}

	s.endErr = proc.ErrDenied
	if err := RestartSwitchedCodexApp(ctx); !errors.Is(err, ErrCodexAppNoEnd) {
		t.Fatalf("denied: %v", err)
	}
	s.endErr = nil

	// one that doesn't end
	endCodexAppServer = func(int) error { return nil }
	if err := RestartSwitchedCodexApp(ctx); !errors.Is(err, ErrCodexAppNoEnd) {
		t.Fatalf("one that stays: %v", err)
	}

	// ended, and the app starts none: said, not success, where the app
	// can't be reopened (macOS: the user does it)
	endCodexAppServer = func(pid int) error {
		s.running = slices.DeleteFunc(s.running, func(p int) bool { return p == pid })
		return nil
	}
	reopened := 0
	CodexAppReopen = func(context.Context) error { reopened++; return errors.New("not here") }
	s.running = []int{7}
	if err := RestartSwitchedCodexApp(ctx); !errors.Is(err, ErrCodexAppNotBack) || reopened != 1 {
		t.Fatalf("none back: %v, reopened %d", err, reopened)
	}
	// the Windows app, whose old app-server's stdout a leftover child
	// holds open, starts none till it is quit and opened again (蛋仔 on
	// Discord): reopened, it is on the new account
	CodexAppReopen = func(context.Context) error { reopened++; return nil }
	s.running = []int{7}
	if err := RestartSwitchedCodexApp(ctx); err != nil || reopened != 2 {
		t.Fatalf("none back on Windows: %v, reopened %d", err, reopened)
	}
	// the user quit the app meanwhile: it opens on the new account
	CodexAppOpen = func(context.Context) (bool, bool) { return false, true }
	s.running = []int{7}
	if err := RestartSwitchedCodexApp(ctx); !errors.Is(err, ErrCodexAppGone) || reopened != 2 {
		t.Fatalf("app quit: %v, reopened %d", err, reopened)
	}
	CodexAppOpen = func(context.Context) (bool, bool) { return true, true }

	// the listing fails: unknown, not success
	s.running, s.listErr = []int{9}, errors.New("ps failed")
	if err := RestartSwitchedCodexApp(ctx); err == nil || errors.Is(err, ErrCodexAppGone) {
		t.Fatalf("a failed listing: %v", err)
	}
}

// TestCodexSwitchAppHelper is TestRestartSwitchedCodexAppReal's fake
// app-server: it waits to be ended.
func TestCodexSwitchAppHelper(t *testing.T) {
	if os.Getenv("UNI_SWITCH_CODEX_SWITCH_HELPER") != "1" {
		return
	}
	os.Stdout.WriteString("running\n")
	time.Sleep(30 * time.Second)
}

// fakeCodexApp runs a real process as the Codex app's app-server, and
// starts another when it ends, as the app does; every one it started ends
// with the test.
type fakeCodexApp struct {
	t       *testing.T
	mu      sync.Mutex
	pid     int
	started []int
	back    bool
}

func (f *fakeCodexApp) start() {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := proc.CommandContext(ctx, os.Args[0], "-test.run=^TestCodexSwitchAppHelper$")
	// TMPDIR is this test's: given no testenv.Marker, the helper makes a
	// test home of its own there, which it never removes once ended
	cmd.Env = []string{"UNI_SWITCH_CODEX_SWITCH_HELPER=1", "TMPDIR=" + f.t.TempDir(), "HOME=" + f.t.TempDir()}
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		f.t.Error(err) // may run on the fake app's goroutine: no Fatal
		return
	}
	done := make(chan struct{})
	f.t.Cleanup(func() { cancel(); <-done })
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "running\n" {
		f.t.Errorf("the helper: %q", line)
		cancel()
		_ = cmd.Wait()
		close(done)
		return
	}
	pid := cmd.Process.Pid
	f.mu.Lock()
	f.pid = pid
	f.started = append(f.started, pid)
	f.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		f.mu.Lock()
		if f.pid == pid {
			f.pid = 0
		}
		back := f.back && ctx.Err() == nil
		f.mu.Unlock()
		close(done)
		if back {
			// the app notices its app-server gone and starts another
			time.Sleep(50 * time.Millisecond)
			f.start()
		}
	}()
}

// With a real process for the app-server, ended by proc.End as the
// Restart ends the real one, the Restart waits for the one the app starts
// next, and says so when the app starts none.
func TestRestartSwitchedCodexAppReal(t *testing.T) {
	stubCodexApp(t)
	oldGone, oldBack := codexAppGoneWait, codexAppBackWait
	codexAppGoneWait, codexAppBackWait = 5*time.Second, 2*time.Second
	t.Cleanup(func() { codexAppGoneWait, codexAppBackWait = oldGone, oldBack })
	f := &fakeCodexApp{t: t, back: true}
	codexAppServersOn = func(context.Context, string) ([]int, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.pid == 0 {
			return nil, nil
		}
		return []int{f.pid}, nil
	}
	endCodexAppServer = proc.End
	f.start()
	first := f.started[0]
	if err := RestartSwitchedCodexApp(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	now := f.pid
	f.mu.Unlock()
	if now == 0 || now == first {
		t.Fatalf("app-server %d after the restart, first %d: want a new one", now, first)
	}

	// the app doesn't start another, and can't be reopened: said
	f.mu.Lock()
	f.back = false
	f.mu.Unlock()
	CodexAppReopen = func(context.Context) error { return errors.New("not here") }
	if err := RestartSwitchedCodexApp(context.Background()); !errors.Is(err, ErrCodexAppNotBack) {
		t.Fatalf("none back: %v", err)
	}
}
