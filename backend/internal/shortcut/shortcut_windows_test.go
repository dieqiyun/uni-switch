package shortcut

import (
	"os"
	"path/filepath"
	"testing"
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
