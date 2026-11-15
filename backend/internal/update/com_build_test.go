package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// launcherFlags are the go build flags `make release-windows` builds the
// launcher with: what stands between "go build" and "-o" on the Makefile
// line that builds ./internal/update/launcher.
func launcherFlags(t *testing.T) []string {
	t.Helper()
	mk, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mk), "\n") {
		if !strings.Contains(line, "./internal/update/launcher") {
			continue
		}
		_, rest, ok := strings.Cut(line, "go build ")
		if !ok {
			continue
		}
		rest, _, _ = strings.Cut(rest, " -o ")
		var flags []string
		for _, f := range regexp.MustCompile(`(?:[^\s"]+|"[^"]*")+`).FindAllString(rest, -1) {
			flags = append(flags, strings.ReplaceAll(f, `"`, ""))
		}
		return flags
	}
	t.Fatal("no line in the Makefile builds ./internal/update/launcher")
	return nil
}

// The launcher is built into every Windows release and written out as
// uni-switch.com wherever its bytes differ from the one there (writeCom). Built
// from a git checkout with Go's default -buildvcs, it carried the release's
// commit and time, so every update wrote a new uni-switch.com though the
// launcher hadn't changed (lucky_eagle_38410 on Discord, 10-28). Built as
// the Makefile builds it, the same source at two commits is the same bytes.
func TestLauncherSameAtEveryCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	flags := launcherFlags(t)
	dir := t.TempDir()
	for name, text := range map[string]string{
		"go.mod":  "module launcher\n\ngo 1.26\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(name string, args ...string) {
		t.Helper()
		cmd := proc.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local",
			"CGO_ENABLED=0", "GOOS=windows", "GOARCH=amd64",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %q: %v\n%s", name, args, err, out)
		}
	}
	build := func(out string) []byte {
		t.Helper()
		run("go", append(append([]string{"build"}, flags...), "-o", out, ".")...)
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	run("git", "init", "-q")
	run("git", "add", "go.mod", "main.go")
	run("git", "commit", "-q", "-m", "one")
	// outside the checkout, so the build sees no untracked file
	out := t.TempDir()
	first := build(filepath.Join(out, "1.com"))
	run("git", "commit", "-q", "--allow-empty", "-m", "two")
	if second := build(filepath.Join(out, "2.com")); !bytes.Equal(first, second) {
		t.Fatalf("the launcher built at the next commit differs (%v): uni-switch.com would be written again on every update", flags)
	}
}
