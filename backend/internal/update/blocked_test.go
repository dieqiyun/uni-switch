package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// relaunchFiles is an update just put in: exe the new version, old the
// running one moved aside.
func relaunchFiles(t *testing.T) (exe, old string) {
	t.Helper()
	dir := t.TempDir()
	exe, old = filepath.Join(dir, "uni-switch.exe"), filepath.Join(dir, "uni-switch.exe.old")
	os.WriteFile(exe, []byte("new version"), 0o755)
	os.WriteFile(old, []byte("running version"), 0o755)
	return exe, old
}

func stubLaunch(t *testing.T, f func(exe string, args, env []string) (<-chan struct{}, func(), error)) {
	t.Helper()
	was := launch
	launch = f
	t.Cleanup(func() { launch = was })
}

func wantPutBack(t *testing.T, exe, old string) {
	t.Helper()
	if b, err := os.ReadFile(exe); err != nil || string(b) != "running version" {
		t.Errorf("%s = %q (%v), want the running version back", filepath.Base(exe), b, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("%s still there: %v", filepath.Base(old), err)
	}
}

// #894: Smart App Control refuses the new, unsigned exe when it is
// started (ERROR_SYSTEM_INTEGRITY_POLICY_VIOLATION): the running version
// goes back in its place, and the error says it was Windows' policy.
func TestRelaunchRefusedPutsBack(t *testing.T) {
	exe, old := relaunchFiles(t)
	stubLaunch(t, func(string, []string, []string) (<-chan struct{}, func(), error) {
		return nil, nil, &os.PathError{Op: "fork/exec", Path: exe, Err: errIntegrityPolicy}
	})
	err := RelaunchBinary(exe, old, true, "settings")
	var be *BlockedError
	if !errors.As(err, &be) || !be.Policy() {
		t.Fatalf("err = %v, want a policy *BlockedError", err)
	}
	if !strings.Contains(err.Error(), "Smart App Control") {
		t.Errorf("err doesn't say why: %v", err)
	}
	wantPutBack(t, exe, old)
}

// A new version that starts and says nothing in time is ended and the
// running one put back; one that quits at once likewise.
func TestRelaunchSilentPutsBack(t *testing.T) {
	was := startWait
	startWait = 200 * time.Millisecond
	t.Cleanup(func() { startWait = was })
	for _, quits := range []bool{false, true} {
		exe, old := relaunchFiles(t)
		var killed atomic.Bool
		stubLaunch(t, func(string, []string, []string) (<-chan struct{}, func(), error) {
			exited := make(chan struct{})
			if quits {
				close(exited)
				return exited, func() {}, nil
			}
			return exited, func() { killed.Store(true); close(exited) }, nil
		})
		err := RelaunchBinary(exe, old, false, "")
		var be *BlockedError
		if !errors.As(err, &be) || be.Policy() {
			t.Fatalf("quits=%v: err = %v, want a *BlockedError not of policy", quits, err)
		}
		if !quits && !killed.Load() {
			t.Error("the silent new version was left running")
		}
		wantPutBack(t, exe, old)
	}
}

// A new version that says it is up stays; what was moved aside is left for
// it to remove.
func TestRelaunchReportedStays(t *testing.T) {
	exe, old := relaunchFiles(t)
	var args []string
	stubLaunch(t, func(_ string, a, env []string) (<-chan struct{}, func(), error) {
		args = a
		for _, kv := range env {
			if p, ok := strings.CutPrefix(kv, startedEnv+"="); ok {
				go func() { time.Sleep(50 * time.Millisecond); os.WriteFile(p, []byte("1"), 0o644) }()
			}
		}
		return make(chan struct{}), func() { t.Error("killed") }, nil
	})
	if err := RelaunchBinary(exe, old, false, ""); err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != "tray" {
		t.Errorf("args = %q", args)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new version" {
		t.Errorf("exe = %q", b)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("old: %v", err)
	}
}

// What the started uni-switch writes is what the one starting it waits for.
func TestReportStarted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "started")
	t.Setenv(startedEnv, p)
	reportStarted()
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(startedEnv) != "" {
		t.Error("left in the environment, for what it starts")
	}
}

// The note of a refused version holds while it is newer than the one
// running, and goes once that has caught up.
func TestBlockedNote(t *testing.T) {
	t.Cleanup(ClearBlocked)
	NoteBlocked("0.1.20", &BlockedError{Err: errIntegrityPolicy})
	b := ReadBlocked("0.1.19")
	if b == nil || b.Version != "0.1.20" || !b.Policy || b.Error == "" {
		t.Fatalf("ReadBlocked = %+v", b)
	}
	if b := ReadBlocked("0.1.20"); b != nil {
		t.Fatalf("caught up: %+v", b)
	}
	if b := ReadBlocked("0.1.19"); b != nil {
		t.Fatalf("not forgotten: %+v", b)
	}
	if policyRefused(syscall.Errno(2)) || !policyRefused(&os.PathError{Err: syscall.Errno(1260)}) {
		t.Error("policyRefused")
	}
}
