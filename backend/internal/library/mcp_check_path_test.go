package library

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// #2685: a bare npx the check's PATH lacks is the one a terminal would run
// (proc.FindTool, the folders a user's tools are installed in, fnm's
// Nodes on Windows among them), and its folder goes on the child's PATH,
// where the node it runs by name is. A path, and a name PATH has, are
// started as they are.
func TestStdioProgramFindsWhatPathLacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	proc.Forget()
	bin := filepath.Join(home, ".local", "bin")
	name := "uni-switch-fake-npx"
	file := name
	if runtime.GOOS == "windows" {
		file += ".cmd"
	}
	testenv.Program(t, filepath.Join(bin, file), "#!/bin/sh\necho hi\n")
	prog, found := stdioProgram(name)
	if !strings.EqualFold(prog, filepath.Join(bin, file)) || !strings.EqualFold(found, bin) {
		t.Fatalf("stdioProgram(%q) = %q, %q; want %q found in %q", name, prog, found, filepath.Join(bin, file), bin)
	}
	abs := filepath.Join(home, "elsewhere", file)
	if prog, found := stdioProgram(abs); prog != abs || found != "" {
		t.Fatalf("a path: stdioProgram = %q, %q; want it as it is", prog, found)
	}
	if prog, found := stdioProgram("uni-switch-no-such-tool"); prog != "uni-switch-no-such-tool" || found != "" {
		t.Fatalf("nowhere: stdioProgram = %q, %q; want the name, for notfound", prog, found)
	}
	t.Setenv("PATH", bin)
	proc.Forget()
	if prog, found := stdioProgram(name); prog != name || found != "" {
		t.Fatalf("on PATH: stdioProgram = %q, %q; want the name as it is", prog, found)
	}
}

func TestWithPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	key := "PATH"
	if runtime.GOOS == "windows" {
		key = "Path" // as os.Environ spells it there
	}
	got := withPath([]string{"A=1", key + "=x" + sep + "y"}, "z")
	if want := []string{"A=1", key + "=x" + sep + "y" + sep + "z"}; !slices.Equal(got, want) {
		t.Fatalf("withPath = %q, want %q", got, want)
	}
	if got := withPath([]string{"A=1"}, "z"); !slices.Equal(got, []string{"A=1", "PATH=z"}) {
		t.Fatalf("no PATH: withPath = %q", got)
	}
}
