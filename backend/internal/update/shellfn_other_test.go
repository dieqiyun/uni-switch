//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The profiles agy's function goes in are the ones each shell reads as a
// terminal opens it: zsh's in ZDOTDIR when that is set, the login shell's
// made when it isn't there, a shell that isn't the terminal's only when
// its profile is.
func TestShellProfiles(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	zdot := filepath.Join(h, ".config", "zsh")
	t.Setenv("ZDOTDIR", zdot)
	oldExe, oldShells := cliExe, cliShells
	t.Cleanup(func() { cliExe, cliShells = oldExe, oldShells })
	cliExe = func() (string, error) { return "/Applications/uni-switch.app/Contents/MacOS/uni-switch", nil }
	cliShells = func() []cliShell {
		return []cliShell{{Name: "zsh", Default: true}, {Name: "bash"}, {Name: "fish"}, {Name: "nu"}}
	}
	bashrc := ".bashrc"
	if runtime.GOOS == "darwin" {
		// no .bash_profile: a login bash reads .profile
		bashrc = ".profile"
		write(t, filepath.Join(h, ".profile"), "umask 022\n")
	} else {
		write(t, filepath.Join(h, ".bashrc"), "umask 022\n")
	}
	in, err := ShellFunction("agy", fnBody)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(zdot, ".zshrc"), filepath.Join(h, bashrc)}
	if strings.Join(in, ",") != strings.Join(want, ",") {
		t.Fatalf("in = %v, want %v", in, want)
	}
	if _, err := os.Stat(filepath.Join(h, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("~/.zshrc was written though ZDOTDIR is set")
	}
	if _, err := os.Stat(filepath.Join(h, ".config", "fish")); !os.IsNotExist(err) {
		t.Fatal("fish's config was made, though fish has never started")
	}

	// fish has started once: its config folder is there
	os.MkdirAll(filepath.Join(h, ".config", "fish"), 0o755)
	in, _ = ShellFunction("agy", fnBody)
	if len(in) != 3 || in[2] != filepath.Join(h, ".config", "fish", "config.fish") {
		t.Fatalf("with fish's folder: %v", in)
	}

	// ZDOTDIR outside the home: not uni-switch's to write
	t.Setenv("ZDOTDIR", t.TempDir())
	in, _ = ShellFunction("agy", fnBody)
	if len(in) != 2 {
		t.Fatalf("ZDOTDIR outside the home: %v", in)
	}
}
