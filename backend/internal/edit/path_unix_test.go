//go:build !windows

package edit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPathRejectsUserSymlinksBelowSystemAlias(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(real, link); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{link, filepath.Join(link, "nested", "config.json")} {
		if e := CheckPath(path); e == nil {
			t.Fatal("accepted user-controlled link", path)
		}
	}
	if e := WriteAtomic(filepath.Join(real, "config.json"), ptr("safe")); e != nil {
		t.Fatal("ordinary file below system temp alias was rejected", e)
	}
	if e := WriteAtomic(filepath.Join(link, "config.json"), ptr("changed")); e == nil {
		t.Fatal("wrote through user-controlled link")
	}
	if b, e := os.ReadFile(filepath.Join(real, "config.json")); e != nil || string(b) != "safe" {
		t.Fatal("link destination changed", e)
	}
}
