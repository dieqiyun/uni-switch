package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// Directory links keep their kind even before their copied target exists,
// or after their source target has gone away.
func TestWindowsSessionDirectorySymlinks(t *testing.T) {
	for _, tc := range []struct {
		name                string
		root, missing, long bool
	}{
		{"target copied later", false, false, false},
		{"dangling directory", false, true, false},
		{"root dangling directory", true, true, false},
		{"long destination", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			from, to := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
			if tc.long {
				to = filepath.Join(dir, strings.Repeat("a", 160), strings.Repeat("b", 160), "destination")
			}
			link := from
			if !tc.root {
				if err := os.MkdirAll(from, 0o700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(from, "a-link") // visited before z-target
			}
			target := filepath.Join(filepath.Dir(link), "z-target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "keep"), []byte("session"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("z-target", link); err != nil {
				if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
					t.Skip("symlinks need Developer Mode or administrator privileges:", err)
				}
				t.Fatal(err)
			}
			if tc.missing {
				if err := os.RemoveAll(target); err != nil {
					t.Fatal(err)
				}
			}
			if err := copyAll(from, to); err != nil {
				t.Fatal(err)
			}
			copied := to
			if !tc.root {
				copied = filepath.Join(to, "a-link")
			}
			fi, err := os.Lstat(copied)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
				t.Error("directory symlink became a file symlink")
			}
			if got, err := os.Readlink(copied); err != nil || got != "z-target" {
				t.Errorf("link target: %q, %v; want z-target", got, err)
			}
			if !tc.missing {
				if b, err := os.ReadFile(filepath.Join(copied, "keep")); err != nil || string(b) != "session" {
					t.Errorf("cannot read through copied directory link: %q, %v", b, err)
				}
			}
		})
	}
}

// An open file prevents rename on Windows but can still be read and copied.
// A sharing violation must leave both the source and destination as they were.
func TestWindowsSessionMoveSharingViolation(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.WriteFile(from, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	err = move(from, to)
	var renameErr *os.LinkError
	if !errors.As(err, &renameErr) || renameErr.Op != "rename" {
		t.Errorf("move error: %v, want the rename error", err)
	}
	if b, err := os.ReadFile(from); err != nil || string(b) != "session" {
		t.Errorf("source changed: %q, %v", b, err)
	}
	if _, err := os.Lstat(to); !os.IsNotExist(err) {
		t.Errorf("destination created despite sharing violation: %v", err)
	}
}
