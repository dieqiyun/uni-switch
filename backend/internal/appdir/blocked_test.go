//go:build !windows

package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

// A ~/.config the user can't write (sudo made it root's) is named as what
// blocks uni-switch; a writable one, or one not there yet under a writable
// home, isn't; and a folder outside the home folder is never named.
func TestBlockedNamesTheFolderInTheWay(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any folder")
	}
	UseExecutable("")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if b := Blocked(); b != "" {
		t.Fatalf("Blocked = %q with nothing made yet, want empty", b)
	}
	cfg := filepath.Join(home, ".config")
	os.Mkdir(cfg, 0o555)
	t.Cleanup(func() { os.Chmod(cfg, 0o755) })
	if b := Blocked(); b != cfg {
		t.Fatalf("Blocked = %q, want %s", b, cfg)
	}
	if !Mine(cfg) {
		t.Fatal("Mine = false for the user's own folder")
	}
	if Own(cfg) || !Own(filepath.Join(cfg, "uni-switch")) || !Own(filepath.Join(cfg, "uni-switch", "x")) {
		t.Fatal("Own: only uni-switch's own folder and what is in it")
	}
	os.Chmod(cfg, 0o755)
	if b := Blocked(); b != "" {
		t.Fatalf("Blocked = %q once writable, want empty", b)
	}
	outside := t.TempDir()
	os.Chmod(outside, 0o555)
	t.Cleanup(func() { os.Chmod(outside, 0o755) })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(outside, "cfg"))
	if b := Blocked(); b != "" {
		t.Fatalf("Blocked = %q outside the home folder, want empty: not the user's to take back", b)
	}
}
