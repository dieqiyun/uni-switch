package dirent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// link makes path a link to the folder target: a junction on Windows, the
// link an agent's folder kept on another drive has there, and a symlink
// elsewhere.
func link(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// A folder, and a link (a junction on Windows) to a folder, are folders to
// read into; a file, a link to a file and a broken link are not.
func TestIsFolder(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "own"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(elsewhere, "kept"), 0o755); err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join(elsewhere, "kept"), filepath.Join(dir, "linked"))
	if err := os.Mkdir(filepath.Join(elsewhere, "gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join(elsewhere, "gone"), filepath.Join(dir, "broken"))
	if err := os.Remove(filepath.Join(elsewhere, "gone")); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"own": true, "linked": true, "file": false, "broken": false}
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(filepath.Join(elsewhere, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(elsewhere, "f"), filepath.Join(dir, "tofile")); err != nil {
			t.Fatal(err)
		}
		want["tofile"] = false
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range es {
		got[e.Name()] = IsFolder(dir, e)
	}
	for name, w := range want {
		if g, ok := got[name]; !ok || g != w {
			t.Errorf("IsFolder(%s) = %v (listed %v), want %v", name, g, ok, w)
		}
	}
}
