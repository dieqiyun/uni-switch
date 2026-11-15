package proc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// A CLI is asked its version once per binary: one that fails to run (macOS
// stopping it as malware, #864) isn't started again until it changes, one
// cut off by the timeout is, and an npm CLI's is read from its package
// without running it.
func TestVersionRunsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	bin := filepath.Join(dir, "codex")
	write := func(body string) {
		testenv.Program(t, bin, "#!/bin/sh\necho x >> '"+runs+"'\n"+body)
	}
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "x")
	}
	write("exit 9\n")
	for range 3 {
		if v := Version(bin); v != "" {
			t.Fatalf("a failing CLI said %q", v)
		}
	}
	if n := count(); n != 1 {
		t.Fatalf("a failing CLI was run %d times", n)
	}
	// changed (reinstalled), it is asked again
	write("echo codex-cli 0.161.1\n")
	os.Chtimes(bin, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if v := Version(bin); v != "codex-cli 0.161.1" {
		t.Fatalf("got %q", v)
	}
	if n := count(); n != 2 {
		t.Fatalf("run %d times", n)
	}

	// a timeout is asked again
	other := filepath.Join(dir, "slow")
	testenv.Program(t, other, "#!/bin/sh\n")
	old := runVersion
	t.Cleanup(func() { runVersion = old })
	asked := 0
	runVersion = func(string) (string, error) { asked++; return "", context.DeadlineExceeded }
	Version(other)
	Version(other)
	if asked != 2 {
		t.Fatalf("a timed-out CLI was asked %d times", asked)
	}

	// an npm CLI: the package says it, nothing is run
	asked = 0
	pkg := filepath.Join(dir, "lib", "node_modules", "@openai", "codex")
	os.MkdirAll(filepath.Join(pkg, "bin"), 0o755)
	os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@openai/codex","version":"0.162.0"}`), 0o644)
	os.WriteFile(filepath.Join(pkg, "bin", "codex.js"), []byte("#!/usr/bin/env node\n"), 0o755)
	link := filepath.Join(dir, "bin", "codex")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(filepath.Join(pkg, "bin", "codex.js"), link); err != nil {
		t.Fatal(err)
	}
	if v := Version(link); v != "0.162.0" || asked != 0 {
		t.Fatalf("npm CLI: %q, run %d times", v, asked)
	}
}

// npm on Windows installs cline as a cline.cmd shim beside node_modules,
// not a link into it. Its version is read from the package the shim names,
// so Cline isn't started, and so never gets to schedule its own npm install
// -g cline@latest (新v学员 on Discord).
func TestVersionReadsAnNpmShim(t *testing.T) {
	old := runVersion
	t.Cleanup(func() { runVersion = old })
	asked := 0
	runVersion = func(string) (string, error) { asked++; return "", nil }
	for _, c := range []struct{ name, shim, pkg, want string }{
		{"cline.cmd", "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\nIF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n)\r\n\r\nendLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\"  \"%dp0%\\node_modules\\cline\\bin\\cline\" %*\r\n", "cline", "3.0.71"},
		{"cline", "#!/bin/sh\nbasedir=$(dirname \"$(echo \"$0\" | sed -e 's,\\\\,/,g')\")\nexec node  \"$basedir/node_modules/cline/bin/cline\" \"$@\"\n", "cline", "3.0.70"},
		{"codex.ps1", "#!/usr/bin/env pwsh\n$basedir=Split-Path $MyInvocation.MyCommand.Definition -Parent\n& \"node$exe\"  \"$basedir/node_modules/@openai/codex/bin/codex.js\" $args\n", "@openai/codex", "0.162.0"},
	} {
		dir := t.TempDir()
		pkg := filepath.Join(dir, "node_modules", filepath.FromSlash(c.pkg))
		os.MkdirAll(pkg, 0o755)
		os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"`+c.pkg+`","version":"`+c.want+`"}`), 0o644)
		bin := filepath.Join(dir, c.name)
		os.WriteFile(bin, []byte(c.shim), 0o755)
		if v := Version(bin); v != c.want || asked != 0 {
			t.Fatalf("%s: %q, run %d times", c.name, v, asked)
		}
	}
}

// A CLI asked its version is told not to update itself: Cline checks on
// every start, --version too.
func TestVersionRunTellsClineNotToUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	bin := filepath.Join(t.TempDir(), "cline")
	testenv.Program(t, bin, "#!/bin/sh\necho \"cline ${CLINE_NO_AUTO_UPDATE:-updates}\"\n")
	if v := Version(bin); v != "cline 1" {
		t.Fatalf("got %q", v)
	}
}
