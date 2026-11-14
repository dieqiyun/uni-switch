package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Cursor CLI's row had no version where Pi's said 1.2.0 (@G_Z_Y on X):
// uni-switch never asked cursor-agent. Its installer's link into
// ~/.local/share/cursor-agent/versions/<v>/ is read with --version, and
// nothing is offered to update it: it updates itself, and uni-switch knows no
// package of it.
func TestCursorCLIVersionShown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	real := filepath.Join(home, ".local/share/cursor-agent/versions/2026.10.01-e373342", exe("cursor-agent"))
	bin := filepath.Join(home, ".local/bin")
	writeAt(t, real, "#!/bin/sh\necho 2026.10.01-e373342\n")
	os.Chmod(real, 0o755)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, exe("cursor-agent"))
	if err := os.Symlink(real, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		writeAt(t, link, "MZ")
	}
	t.Setenv("PATH", bin)
	versions = memo{}
	t.Cleanup(func() { versions = memo{} })
	wasRun := runVersion
	runVersion = func(string) string { return "2026.10.01-e373342\n" }
	t.Cleanup(func() { runVersion = wasRun })

	a := cursor(home)
	c, ok := a.CLI()
	if !ok || c.Version != "2026.10.01-e373342" {
		t.Fatalf("Cursor CLI's version = %q (ok %v), want 2026.10.01-e373342", c.Version, ok)
	}
	if c.Update || c.Via != "" || c.Command != "" {
		t.Errorf("an update is offered for a CLI that updates itself: %+v", c)
	}
	if got := installCommands("cursor", runtime.GOOS, true); got != nil {
		t.Errorf("an install command for Cursor CLI: %+v", got)
	}
}
