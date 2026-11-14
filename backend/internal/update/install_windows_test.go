package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// What the last update moved aside can still be running (a `uni-switch serve`
// started before it), and then can't be removed or replaced: the running
// exe goes beside it and the new version still goes in.
func TestInstallBinaryBesideARunningOld(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
	b, err := os.ReadFile(ping)
	if err != nil {
		t.Skip("no PING.EXE:", err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "uni-switch.exe")
	for _, f := range []string{exe, exe + ".old"} {
		if err := os.WriteFile(f, b, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(f, "-n", "30", "127.0.0.1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	}
	if os.Remove(exe+".old") == nil {
		t.Fatal("a running .old could be removed; the test proves nothing")
	}
	staged := exe + ".new"
	os.WriteFile(staged, []byte("new version"), 0o755)
	if err := InstallBinary(staged, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new version" {
		t.Fatalf("exe is not the new version: %d bytes", len(got))
	}
	if _, err := os.Stat(exe + ".old-2"); err != nil {
		t.Fatal("the running exe was not moved beside the old:", err)
	}
}

// holdOpen opens path as an antivirus scan does: reading, without letting
// it be renamed or removed (no FILE_SHARE_DELETE).
func holdOpen(t *testing.T, path string) syscall.Handle {
	p, _ := syscall.UTF16PtrFromString(path)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Held open for a moment, the exe still goes aside and the new version in;
// held for good, the update says what holds it up and keeps the download.
func TestInstallBinaryHeldOpen(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "uni-switch-windows-amd64.exe")
	staged := exe + ".new"
	os.WriteFile(exe, []byte("old version"), 0o755)
	os.WriteFile(staged, []byte("new version"), 0o755)

	h := holdOpen(t, exe)
	if os.Rename(exe, exe+".x") == nil {
		t.Fatal("a held exe could be renamed; the test proves nothing")
	}
	go func() { time.Sleep(500 * time.Millisecond); syscall.CloseHandle(h) }()
	if err := InstallBinary(staged, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new version" {
		t.Fatalf("exe is %q", got)
	}

	os.WriteFile(staged, []byte("newer version"), 0o755)
	held := heldFor
	heldFor = 0 // held for good: no use waiting it out
	t.Cleanup(func() { heldFor = held })
	h = holdOpen(t, exe)
	defer syscall.CloseHandle(h)
	err := InstallBinary(staged, exe)
	if err == nil || !strings.Contains(err.Error(), "another program has it open") {
		t.Fatalf("held for good: %v", err)
	}
	t.Log(err)
	if _, err := os.Stat(staged); err != nil {
		t.Fatal("the download was not kept for another try:", err)
	}
}
