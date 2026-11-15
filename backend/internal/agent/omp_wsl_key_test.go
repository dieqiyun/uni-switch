package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// The probe finds an omp bun installed though ~/.bun/bin is put on PATH
// only by ~/.bashrc, which sh -l doesn't read, and asks it its version
// with bun on PATH for its #!/usr/bin/env bun (whqtian on Discord: a WSL
// omp's version stayed unknown, so max stayed xhigh).
func TestWSLProbeFindsBunOmp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe runs in a Linux distro")
	}
	to, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("no timeout(1)")
	}
	home := t.TempDir()
	bun := filepath.Join(home, ".bun", "bin")
	sys := filepath.Join(home, "sys") // PATH: only timeout, nothing of omp's
	for _, d := range []string{bun, sys, filepath.Join(home, ".omp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testenv.Program(t, filepath.Join(bun, "bun"), "#!/bin/sh\necho omp/16.5.1\n")
	// a program too: the probe runs it under its own timeout 10, which a
	// newly written file's first-run check on macOS outlasts under load
	testenv.Program(t, filepath.Join(bun, "omp"), "#!/usr/bin/env bun\n")
	if err := os.Symlink(to, filepath.Join(sys, "timeout")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", wslProbeScript)
	cmd.Env = []string{"HOME=" + home, "PATH=" + sys + ":/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	d := parseProbe("U", string(out))
	if d == nil || !d.Has["bin:omp"] || d.Versions["omp"] != "16.5.1" {
		t.Fatalf("probe said:\n%s\nread as %+v", out, d)
	}
	if !ompTakesMax(d.place("omp@wsl:U").version) {
		t.Error("its models wouldn't offer max")
	}
}
