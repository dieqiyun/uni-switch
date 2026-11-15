package shortcut

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// A shortcut is made, left be while it opens this exe, and pointed at the
// exe again once that was moved.
func TestWrite(t *testing.T) {
	dir := t.TempDir()
	lnk := filepath.Join(dir, Name+".lnk")
	exe := filepath.Join(dir, "a", "uni-switch.exe")
	moved := filepath.Join(dir, "b", "uni-switch.exe")
	for _, f := range []string{exe, moved} {
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte("MZ"), 0o755)
	}
	for i, c := range []struct {
		exe     string
		written bool
	}{{exe, true}, {exe, false}, {moved, true}, {moved, false}} {
		written, err := write(lnk, c.exe)
		if err != nil {
			t.Fatal(i, err)
		}
		if written != c.written {
			t.Fatalf("%d: written = %v, want %v", i, written, c.written)
		}
	}
	if _, err := os.Stat(lnk); err != nil {
		t.Fatal("no shortcut:", err)
	}
}

func TestWriteShortAndLongPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a folder with a long name")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "uni-switch.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if _, err := windows.GetShortPathName(path, &buf[0], uint32(len(buf))); err != nil {
		t.Fatal(err)
	}
	short := windows.UTF16ToString(buf)
	long, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(short, long) {
		t.Skip("8.3 aliases are disabled on this test volume")
	}
	lnk := filepath.Join(dir, Name+".lnk")
	if made, err := write(lnk, short); err != nil || !made {
		t.Fatalf("first shortcut: made=%v, err=%v", made, err)
	}
	if made, err := write(lnk, long); err != nil || made {
		t.Fatalf("the same target was rewritten through its long spelling: made=%v, err=%v", made, err)
	}
	other := filepath.Join(dir, "another.exe")
	if err := os.Link(exe, other); err != nil {
		t.Fatal(err)
	}
	if !Stale(other, dir, exe) {
		t.Fatal("a different hard-link pathname must still be repointed")
	}
}
