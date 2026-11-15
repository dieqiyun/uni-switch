package steady

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// a reader holding the file without FILE_SHARE_DELETE, as other programs
// open it, for a moment: os.Rename over it is refused, Rename waits it out
func TestRenameOverAFileHeldOpen(t *testing.T) {
	dir := t.TempDir()
	p, tmp := filepath.Join(dir, "logins.json"), filepath.Join(dir, "logins.json.tmp")
	os.WriteFile(p, []byte("old"), 0o600)
	os.WriteFile(tmp, []byte("new"), 0o600)
	name, _ := syscall.UTF16PtrFromString(p)
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, p); err == nil {
		t.Fatal("renamed over a file held open: nothing to wait out")
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		syscall.CloseHandle(h)
	}()
	if err := Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadFile(p); err != nil || string(b) != "new" {
		t.Fatalf("read %q, %v", b, err)
	}
}

// and a file held so refuses to be read until let go
func TestReadAFileHeldOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plugin-auth.json")
	os.WriteFile(p, []byte("accounts"), 0o600)
	name, _ := syscall.UTF16PtrFromString(p)
	h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(p); err == nil {
		t.Fatal("read a file held unshared: nothing to wait out")
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		syscall.CloseHandle(h)
	}()
	if b, err := ReadFile(p); err != nil || string(b) != "accounts" {
		t.Fatalf("read %q, %v", b, err)
	}
	if _, err := ReadFile(filepath.Join(filepath.Dir(p), "none")); !os.IsNotExist(err) {
		t.Fatalf("a file not there: %v", err)
	}
}

// Windows itself tells a file whose folder isn't there (#1665)
func TestNoFolderIsPathNotFound(t *testing.T) {
	looks := 0
	stat = func(p string) (fs.FileInfo, error) {
		looks++
		return os.Stat(p)
	}
	defer func() { stat = os.Stat }()
	if _, err := Stat(filepath.Join(t.TempDir(), "agent", "settings.json")); !errors.Is(err, fs.ErrNotExist) || looks != 1 {
		t.Fatalf("looked %d times: %v", looks, err)
	}
	looks = 0
	if _, err := Stat(filepath.Join(t.TempDir(), "settings.json")); !errors.Is(err, fs.ErrNotExist) || looks < 2 {
		t.Fatalf("a file alone not there looked at %d times: %v", looks, err)
	}
}
