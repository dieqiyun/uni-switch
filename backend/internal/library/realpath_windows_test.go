package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRealDirShortPathHasOneIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a folder with a long name")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if _, err := windows.GetShortPathName(path, &buf[0], uint32(len(buf))); err != nil {
		t.Fatal(err)
	}
	short := windows.UTF16ToString(buf)
	long, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(short, long) {
		t.Skip("8.3 aliases are disabled on this test volume")
	}
	for _, p := range []string{short, long} {
		if got := realDir(p); got != long {
			t.Errorf("one directory has multiple identities: %q resolved to %q, want %q", p, got, long)
		}
	}
}
