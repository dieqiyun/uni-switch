package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// holdWritable opens path as a scanner checking a new program may: for
// writing, sharing no delete, so Windows neither removes it nor starts a
// process from it while the handle is open.
func holdWritable(t *testing.T, path string) func() {
	t.Helper()
	p, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	letGo := func() {
		if !closed {
			closed = true
			syscall.CloseHandle(h)
		}
	}
	t.Cleanup(letGo)
	return letGo
}

func pingBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE"))
	if err != nil {
		t.Skip("no PING.EXE:", err)
	}
	return b
}

// A real open handle on the earlier download: Windows won't remove it
// (what mycf_711's update dropped the error of), and the next download
// goes under a name of its own instead of asking for bytes past its end.
func TestStageStepsRoundAFileHeldOpen(t *testing.T) {
	quickRetries(t)
	f := newDesktopFixture(t)
	srv, ranges := releaseHost(t, f.body, nil)
	f.asset = asset(srv.URL, f.body)
	d := cacheStage(f.exe)
	os.MkdirAll(d, 0o755)
	left := filepath.Join(d, "uni-switch.exe.new")
	os.WriteFile(left, f.body, 0o755) // the whole of it, as the reporter's
	holdWritable(t, left)
	if os.Remove(left) == nil {
		t.Fatal("a held file could be removed; the test proves nothing")
	}
	staged, err := stageBinaryFor(context.Background(), f.asset, f.exe)
	if err != nil {
		t.Fatalf("stage: %v (asked %q)", err, ranges())
	}
	if staged == left {
		t.Fatalf("staged at the held %s", left)
	}
	if b, _ := os.ReadFile(staged); !bytes.Equal(b, f.body) {
		t.Fatalf("staged %q", b)
	}
	if err := download(context.Background(), f.asset, left); err == nil || !strings.Contains(err.Error(), "another program has it open") {
		t.Fatalf("download into the held file: %v, want it said held", err)
	}
}

// The trial start of a download a scanner still holds (Windows: "being
// used by another process") waits for it to let go, as moveIn does, and
// the update goes in: it was never refused.
func TestTrialWaitsForAHeldDownload(t *testing.T) {
	body := pingBytes(t)
	was := canStart
	canStart = trialStart
	t.Cleanup(func() { canStart = was })
	waits, held := asideWaits, heldFor
	asideWaits, heldFor = []time.Duration{50 * time.Millisecond}, 20*time.Second
	t.Cleanup(func() { asideWaits, heldFor = waits, held })

	dir := t.TempDir()
	exe := filepath.Join(dir, "uni-switch.exe")
	staged := filepath.Join(dir, "uni-switch.exe.new")
	os.WriteFile(exe, []byte("old version"), 0o755)
	os.WriteFile(staged, body, 0o755)
	letGo := holdWritable(t, staged)
	if err := trialStart(staged); !heldOpen(err) {
		t.Fatalf("trial start of a held file: %v, want it held; the test proves nothing", err)
	}
	go func() { time.Sleep(700 * time.Millisecond); letGo() }()
	if _, err := InstallBinaryAside(staged, exe); err != nil {
		t.Fatalf("InstallBinaryAside: %v", err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, body) {
		t.Fatal("the new version isn't in place")
	}
}

// Held past the wait, the update says what holds it and keeps the
// download for a later restart; it isn't noted as a version this computer
// won't start (*BlockedError), which stopped the automatic checks.
func TestTrialHeldForGoodIsNotBlocked(t *testing.T) {
	body := pingBytes(t)
	was := canStart
	canStart = trialStart
	t.Cleanup(func() { canStart = was })
	shortWaits(t)

	dir := t.TempDir()
	exe := filepath.Join(dir, "uni-switch.exe")
	staged := filepath.Join(dir, "uni-switch.exe.new")
	os.WriteFile(exe, []byte("old version"), 0o755)
	os.WriteFile(staged, body, 0o755)
	letGo := holdWritable(t, staged)
	_, err := InstallBinaryAside(staged, exe)
	if err == nil || errors.As(err, new(*BlockedError)) || !strings.Contains(err.Error(), "another program still has the download open") {
		t.Fatalf("InstallBinaryAside = %v, want it said held, not blocked", err)
	}
	letGo()
	wantInstallFile(t, exe, "old version")
	if b, _ := os.ReadFile(staged); !bytes.Equal(b, body) {
		t.Fatal("the download wasn't kept for another try")
	}
	if _, err := InstallBinaryAside(staged, exe); err != nil {
		t.Fatalf("the restart after it let go: %v", err)
	}
}
