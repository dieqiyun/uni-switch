package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fnBody is a function body that says the shell and the program.
func fnBody(kind, prog string) string {
	return "agy() { # " + kind + "\n  '" + prog + "' agy run \"$@\"\n}"
}

// The block uni-switch puts in a shell's profile for agy (bestony on Discord):
// added after the user's own lines, the same file again when nothing
// changed, rewritten in place, taken out leaving the file as it was, and
// never written where the file can't be read or outside the home.
func TestShellFunction(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	exe := filepath.Join(h, "Applications", "uni-switch")
	rc := filepath.Join(h, ".zshrc")
	fish := filepath.Join(h, ".config", "fish", "config.fish")
	crlf := filepath.Join(h, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	broken := filepath.Join(h, ".bashrc")
	outside := filepath.Join(t.TempDir(), ".zshrc")
	profiles := []ShellProfile{
		{Kind: "posix", Path: rc},
		{Kind: "fish", Path: fish},
		{Kind: "powershell", Path: crlf},
		{Kind: "posix", Path: broken},
		{Kind: "posix", Path: outside, Create: true},
	}
	oldExe, oldProfiles := cliExe, fnProfiles
	t.Cleanup(func() { cliExe, fnProfiles = oldExe, oldProfiles })
	cliExe = func() (string, error) { return exe, nil }
	fnProfiles = func() []ShellProfile { return profiles }

	mine := "export PATH=$HOME/bin:$PATH\nalias ll='ls -l'" // no newline at the end
	write(t, rc, mine)
	ps := "\xef\xbb\xbfSet-Alias ll Get-ChildItem\r\n"
	write(t, crlf, ps)
	// a folder where .bashrc is: its read fails, so what it holds is unknown
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}

	in, err := ShellFunction("agy", fnBody)
	if err == nil {
		t.Fatal("the unreadable .bashrc gave no error")
	}
	if strings.Join(in, ",") != rc+","+crlf {
		t.Fatalf("in = %v", in)
	}
	want := mine + "\n\n# >>> uni-switch agy >>>\nagy() { # posix\n  '" + exe + "' agy run \"$@\"\n}\n# <<< uni-switch agy <<<\n"
	if got := read(t, rc); got != want {
		t.Fatalf(".zshrc:\n%s\nwant:\n%s", got, want)
	}
	if got := read(t, crlf); !strings.HasPrefix(got, ps+"\r\n# >>> uni-switch agy >>>\r\nagy() { # powershell\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("PowerShell's profile lost its BOM or CRLF: %q", got)
	}
	if _, err := os.Stat(fish); !os.IsNotExist(err) {
		t.Fatal("fish's profile was made, though fish isn't the shell a terminal opens")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("a profile outside the home was written")
	}
	if got := ShellFunctionIn("agy"); strings.Join(got, ",") != rc+","+crlf {
		t.Fatalf("ShellFunctionIn = %v", got)
	}

	// again: nothing changes, the file isn't written
	st, _ := os.Stat(rc)
	old := st.ModTime().Add(-time.Hour)
	os.Chtimes(rc, old, old)
	if _, err := ShellFunction("agy", fnBody); err == nil {
		t.Fatal("no error the second time")
	}
	if got := read(t, rc); got != want {
		t.Fatalf("second time:\n%s", got)
	}
	if st, _ := os.Stat(rc); !st.ModTime().Equal(old) {
		t.Fatal("the same block was written again")
	}

	// the user adds lines after it; the block is rewritten where it is
	write(t, rc, want+"export EDITOR=vim\n")
	exe = filepath.Join(h, "moved", "uni-switch")
	ShellFunction("agy", fnBody)
	if got := read(t, rc); got != strings.ReplaceAll(want, filepath.Join(h, "Applications"), filepath.Join(h, "moved"))+"export EDITOR=vim\n" {
		t.Fatalf("rewritten:\n%s", got)
	}

	// out: the user's lines as they were
	if err := DropShellFunction("agy"); err == nil {
		t.Fatal("no error dropping with the unreadable .bashrc")
	}
	if got := read(t, rc); got != mine+"\nexport EDITOR=vim\n" {
		t.Fatalf("after Drop:\n%q", got)
	}
	if got := read(t, crlf); got != ps {
		t.Fatalf("PowerShell's after Drop: %q", got)
	}
	if got := ShellFunctionIn("agy"); len(got) != 0 {
		t.Fatalf("ShellFunctionIn after Drop = %v", got)
	}

	// the shell a terminal opens gets a profile made for it
	profiles[1].Create = true
	ShellFunction("agy", fnBody)
	if got := read(t, fish); got != "# >>> uni-switch agy >>>\nagy() { # fish\n  '"+exe+"' agy run \"$@\"\n}\n# <<< uni-switch agy <<<\n" {
		t.Fatalf("made fish profile:\n%s", got)
	}
}

// withFnBlock's cases: the file's own ending, a block whose close line was
// cut, another tool's block left alone.
func TestWithFnBlock(t *testing.T) {
	for _, c := range []struct{ name, in, body, want string }{
		{"empty", "", "x", "# >>> uni-switch agy >>>\nx\n# <<< uni-switch agy <<<\n"},
		{"ends with newline", "a\n", "x", "a\n\n# >>> uni-switch agy >>>\nx\n# <<< uni-switch agy <<<\n"},
		{"out", "a\n\n# >>> uni-switch agy >>>\nx\n# <<< uni-switch agy <<<\n", "", "a\n"},
		{"out, between lines", "a\n\n# >>> uni-switch agy >>>\nx\n# <<< uni-switch agy <<<\nb\n", "", "a\nb\n"},
		{"close cut", "a\n# >>> uni-switch agy >>>\nb\n", "", "a\nb\n"},
		{"another's block", "# >>> conda initialize >>>\nc\n# <<< conda initialize <<<\n", "", "# >>> conda initialize >>>\nc\n# <<< conda initialize <<<\n"},
		{"none to drop", "a", "", "a"},
	} {
		if got := withFnBlock(c.in, "agy", c.body, false); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
