package edit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// While Filtered runs, what this package writes passes through its filter,
// given the file written (a link's target); once it returns, writes are as
// they were.
func TestFilteredWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	link := filepath.Join(dir, "link.json")
	os.WriteFile(path, []byte(`{"k":"x"}`), 0o644)
	if err := os.Symlink(path, link); err != nil {
		t.Skip(err)
	}
	var seen string
	err := Filtered(func(p string, b []byte) []byte {
		seen = p
		return bytes.ReplaceAll(b, []byte("uni-switch"), []byte("kept"))
	}, func() error { return SetJSON(link, KV{Path: "k", Value: "uni-switch"}) })
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := GetJSON(path, "k"); got != "kept" {
		t.Errorf("filtered write: %q", got)
	}
	if want, _ := filepath.EvalSymlinks(path); seen != path && seen != want {
		t.Errorf("filter given %s, not %s", seen, path)
	}
	if err := SetJSON(path, KV{Path: "k", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetJSON(path, "k"); got != "uni-switch" {
		t.Errorf("filter still on after Filtered: %q", got)
	}
}
