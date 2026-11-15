package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Copying after a permission error can empty the source before removing its
// folder fails. Only a cross-device rename may use the copy fallback.
func TestMoveRenameFailureKeepsSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions; Windows tests use an open file")
	}
	dir := t.TempDir()
	parent := filepath.Join(dir, "locked")
	from, to := filepath.Join(parent, "source"), filepath.Join(dir, "destination")
	if err := os.MkdirAll(from, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(from, "keep")
	if err := os.WriteFile(file, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o700) })
	if err := os.Rename(from, filepath.Join(dir, "probe")); err == nil {
		t.Skip("directory permissions do not prevent renames here")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("probe rename: %v", err)
	}
	err := move(from, to)
	var renameErr *os.LinkError
	if !errors.As(err, &renameErr) || renameErr.Op != "rename" {
		t.Errorf("move error: %v, want the rename error", err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "session" {
		t.Errorf("source changed: %q, %v", b, err)
	}
	if _, err := os.Lstat(to); !os.IsNotExist(err) {
		t.Errorf("destination created despite rename failure: %v", err)
	}
}
