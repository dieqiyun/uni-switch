//go:build !windows

package edit

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A file shared with a group keeps that group, and its mode, when written
// (Aside's group-writable models.json, #1499): the temp file renamed over
// it would otherwise take its folder's group.
func TestWriteAtomicKeepsGroupAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, []byte(`{"keepMe":"yes"}`), 0o664); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	gid := func(p string) int {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return int(st.Sys().(*syscall.Stat_t).Gid)
	}
	groups, _ := os.Getgroups()
	other := -1
	for _, g := range groups {
		if g != gid(path) && os.Chown(path, -1, g) == nil {
			other = g
			break
		}
	}
	if other < 0 {
		t.Skip("the user is in no second group to give the file")
	}
	if err := WriteAtomic(path, []byte(`{"keepMe":"yes","providers":{}}`)); err != nil {
		t.Fatal(err)
	}
	if got := gid(path); got != other {
		t.Fatalf("group %d, want %d", got, other)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o664 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}
