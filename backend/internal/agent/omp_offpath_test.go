package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// An omp bun installed is asked its version though ~/.bun/bin isn't on
// uni-switch's PATH, as on Windows and in WSL (whqtian on Discord: on Ubuntu
// omp's models still offered xhigh in place of max). A `uni-switch serve`, or
// a service started with a PATH that lacks it, found no omp at all, so its
// version stayed unknown and was taken for one older than 16.4.0.
func TestOmpVersionOffPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bun's Windows launcher is an .exe beside a .bunx")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	empty := filepath.Join(home, "empty")
	pkg := filepath.Join(home, ".bun/install/global/node_modules/@oh-my-pi/pi-coding-agent")
	for _, d := range []string{empty, filepath.Join(pkg, "dist"), filepath.Join(home, ".bun/bin"), filepath.Join(home, ".omp/agent")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@oh-my-pi/pi-coding-agent","version":"18.9.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "dist/cli.js"), []byte("#!/usr/bin/env bun\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../install/global/node_modules/@oh-my-pi/pi-coding-agent/dist/cli.js", filepath.Join(home, ".bun/bin/omp")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", empty)
	proc.Forget()
	t.Cleanup(proc.Forget)
	v := ompVersion()
	if v != "18.9.1" {
		t.Fatalf("omp's version read as %q, want 18.9.1", v)
	}
	if !ompTakesMax(v) {
		t.Error("its models wouldn't offer max")
	}
}
