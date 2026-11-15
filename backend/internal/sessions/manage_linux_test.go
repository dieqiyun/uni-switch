package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// /dev/shm normally lives on another filesystem from TempDir. Check EXDEV
// before exercising a real cross-device move in both directions.
func TestMoveCrossDevice(t *testing.T) {
	dir := t.TempDir()
	other, err := os.MkdirTemp("/dev/shm", "uni-switch-session-")
	if err != nil {
		t.Skip("no writable shared-memory filesystem:", err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	from, to := filepath.Join(dir, "source"), filepath.Join(other, "destination")
	if err := os.Mkdir(from, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "keep"), []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("keep", filepath.Join(from, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err == nil {
		t.Skip("temporary folders are on the same filesystem")
	} else if !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("probe rename: %v", err)
	}
	for _, paths := range [][2]string{{from, to}, {to, from}} {
		if err := move(paths[0], paths[1]); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(paths[0]); !os.IsNotExist(err) {
			t.Errorf("source still exists: %v", err)
		}
		if got, err := os.Readlink(filepath.Join(paths[1], "link")); err != nil || got != "keep" {
			t.Errorf("link target: %q, %v; want keep", got, err)
		}
		if b, err := os.ReadFile(filepath.Join(paths[1], "link")); err != nil || string(b) != "session" {
			t.Errorf("session content: %q, %v", b, err)
		}
	}
}
