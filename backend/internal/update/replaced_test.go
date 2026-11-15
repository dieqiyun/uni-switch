package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReplacedAndStaleNew(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "uni-switch.exe")
	os.WriteFile(exe, []byte("v1"), 0o755)
	started, _ := os.Stat(exe)
	if Replaced(exe, started) {
		t.Fatal("replaced before anything happened")
	}
	// another uni-switch moves it aside and puts the new one in
	os.Rename(exe, exe+".old")
	os.WriteFile(exe, []byte("v2 build"), 0o755)
	if !Replaced(exe, started) {
		t.Fatal("not seen as replaced")
	}
	// the same size, a later time
	os.WriteFile(exe, []byte("v1"), 0o755)
	os.Chtimes(exe, time.Now(), started.ModTime().Add(time.Minute))
	if !Replaced(exe, started) {
		t.Fatal("a same-size update not seen")
	}

	// a .new that is exe over again goes; another stays
	os.WriteFile(exe+".new", []byte("v1"), 0o755)
	RemoveStaleNew(exe)
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("a .new the same as exe was kept")
	}
	os.WriteFile(exe+".new", []byte("v3"), 0o755)
	RemoveStaleNew(exe)
	if _, err := os.Stat(exe + ".new"); err != nil {
		t.Fatal("a different .new was removed")
	}
}
