package library

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// What cargo printed on the Windows test box (cargo 1.99.1, a Chinese
// Windows) when the rtk.exe it was to replace was running.
const cargoInUseOut = "   Compiling rtk v1.1.0\r\n    Finished `release` profile [optimized] target(s) in 1m 02s\r\n" +
	"  Replacing C:\\Users\\yeton\\rtkcargo-sb\\home\\.cargo\\bin\\rtk.exe\r\n" +
	"error: failed to move `C:\\Users\\yeton\\rtkcargo-sb\\home\\.cargo\\bin\\cargo-installQ79RGS\\rtk.exe` to `C:\\Users\\yeton\\rtkcargo-sb\\home\\.cargo\\bin\\rtk.exe`\r\n" +
	"\r\nCaused by:\r\n  拒绝访问。 (os error 5)\r\n"

// What it printed building rtk with the windows-gnu toolchain and no
// dlltool: the cause is three lines above the last.
const cargoDlltoolOut = "   Compiling paste v1.0.15\r\n   Compiling adler2 v2.0.1\r\n" +
	"error: error calling dlltool 'dlltool.exe': program not found\r\n" +
	"   Compiling colorchoice v1.0.5\r\n" +
	"error: could not compile `parking_lot_core` (lib) due to 1 previous error\r\n" +
	"warning: build failed, waiting for other jobs to finish...\r\n" +
	"error: could not compile `windows-sys` (lib) due to 1 previous error\r\n" +
	"error: failed to compile `rtk v1.1.0 (https://github.com/rtk-ai/rtk?branch=master#753a3b0e)`, intermediate artifacts can be found at `C:\\Users\\yeton\\AppData\\Local\\Temp\\cargo-installJjS3qF`.\r\n" +
	"To reuse those artifacts with a future compilation, set the environment variable `CARGO_BUILD_BUILD_DIR` to that path.\r\n"

func onWindows(t *testing.T, yes bool) {
	t.Helper()
	old := rtkOnWindows
	rtkOnWindows = yes
	t.Cleanup(func() { rtkOnWindows = old })
}

// A uni-switch whose PATH has no cargo — started before Rust was installed, or
// from a shortcut whose PATH rustup never touched — upgrades the rtk in
// cargo's folder with the cargo rustup put beside it, by its path. It had
// no upgrade at all, and its Upgrade button did nothing (新v学员).
func TestRTKCargoBesideRTK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	latestServer(t, "v1.1.0")
	dir := filepath.Join(h, ".cargo", "bin")
	testenv.Program(t, filepath.Join(dir, "rtk"), strings.ReplaceAll(versionedRTK, "VERSION", filepath.Join(dir, "VERSION")))
	write(t, filepath.Join(dir, "VERSION"), "1.0.0")
	testenv.Program(t, filepath.Join(dir, "cargo"), `#!/bin/sh
[ "$*" = "install --git https://github.com/rtk-ai/rtk --branch master rtk --force" ] || exit 2
echo 1.1.0 > "`+filepath.Join(dir, "VERSION")+`"
`)
	t.Setenv("PATH", filepath.Join(h, "bin")) // no cargo, no rtk

	v := ReadRTK()
	if v.Path != filepath.Join(dir, "rtk") || v.Upgrade == "" || !strings.Contains(v.Upgrade, "--branch master") || v.NoUpgrade != "" {
		t.Fatalf("view: %+v", v)
	}
	if c := rtkUpgrader(v.Path); len(c) == 0 || c[0] != filepath.Join(dir, "cargo") {
		t.Fatalf("upgrader %q, want cargo by its path", c)
	}
	if v, err := UpgradeRTK(); err != nil || v.Version != "1.1.0" {
		t.Fatalf("after: %+v, %v", v, err)
	}
}

// With no cargo anywhere, the card says so and what to run, and Upgrade's
// error does too — not "uni-switch can't tell how this RTK was installed".
func TestRTKCargoMissingSaysSo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	latestServer(t, "v1.1.0")
	dir := filepath.Join(h, ".cargo", "bin")
	testenv.Program(t, filepath.Join(dir, "rtk"), strings.ReplaceAll(versionedRTK, "VERSION", filepath.Join(dir, "VERSION")))
	write(t, filepath.Join(dir, "VERSION"), "1.0.0")
	t.Setenv("PATH", filepath.Join(h, "bin"))

	v := ReadRTK()
	if v.Path != filepath.Join(dir, "rtk") || v.Upgrade != "" || v.NoUpgrade != "cargo" || v.Manual != rtkCargoCmd {
		t.Fatalf("view: %+v", v)
	}
	_, err := UpgradeRTK()
	if err == nil || !strings.Contains(err.Error(), "installed with cargo") || !strings.Contains(err.Error(), "rustup.rs") || !strings.Contains(err.Error(), rtkCargoCmd) {
		t.Fatalf("error: %v", err)
	}
}

// CARGO_HOME moves cargo's folder (D:\Rust\.cargo is common): an rtk there
// is cargo's too, and on Windows its path matches whatever its case.
func TestRTKCargoHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	ch := filepath.Join(h, "Rust", ".cargo")
	t.Setenv("CARGO_HOME", ch)
	dir := filepath.Join(ch, "bin")
	testenv.Program(t, filepath.Join(dir, "rtk"), strings.ReplaceAll(versionedRTK, "VERSION", filepath.Join(dir, "VERSION")))
	write(t, filepath.Join(dir, "VERSION"), "1.0.0")
	testenv.Program(t, filepath.Join(dir, "cargo"), "#!/bin/sh\n")
	t.Setenv("PATH", filepath.Join(h, "bin"))

	if p := rtkPath(); p != filepath.Join(dir, "rtk") {
		t.Fatalf("rtkPath %q", p)
	}
	if c := rtkUpgrader(filepath.Join(dir, "rtk")); len(c) == 0 || c[0] != filepath.Join(dir, "cargo") {
		t.Fatalf("upgrader %q", c)
	}
	onWindows(t, true)
	if !inCargoBin(strings.ToUpper(dir)) || !inCargoBin(filepath.Join(h, ".cargo", "bin")) {
		t.Fatal("Windows: cargo's folders by another case")
	}
	onWindows(t, false)
	if inCargoBin(strings.ToUpper(dir)) {
		t.Fatal("elsewhere the case counts")
	}
}

// On Windows cargo can't replace an rtk.exe that is running: the rtk is
// moved aside, a copy put in its place, and cargo run again, which
// installs over the copy. Anywhere else, and for any other failure,
// cargo's failure stands.
func TestRTKCargoInUseMovesAside(t *testing.T) {
	h := sandbox(t)
	bin := filepath.Join(h, ".cargo", "bin", "rtk.exe")
	write(t, bin, "old rtk")
	write(t, bin+".old-1", "an earlier upgrade's")
	cargo := []string{"cargo", "install", "--git", "x", "rtk", "--force"}

	run := func(fails int, out string) (int, []string) {
		var envs []string
		n := 0
		p := runPlain
		runPlain = func(c []string, _ time.Duration, env ...string) (string, error) {
			n++
			envs = append(envs, env...)
			if n <= fails {
				return out, installFailed(c, out, 101, errors.New("exit status 101"))
			}
			if b, err := os.ReadFile(bin); err != nil || string(b) != "old rtk" {
				t.Errorf("cargo ran over %q, %v; want the copy", b, err)
			}
			return "", os.WriteFile(bin, []byte("new rtk"), 0o755)
		}
		t.Cleanup(func() { runPlain = p })
		return n, envs
	}

	onWindows(t, true)
	run(1, cargoInUseOut)
	if err := runCargoRTK(cargo, bin, time.Minute); err != nil {
		t.Fatalf("in use: %v", err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "new rtk" {
		t.Fatalf("rtk.exe: %q", b)
	}
	if exists(bin + ".old-1") {
		t.Fatal("an earlier upgrade's rtk left behind")
	}
	aside := bin + ".old-" + strconv.Itoa(os.Getpid())
	if exists(aside) {
		t.Fatal("this upgrade's rtk left behind though nothing runs it")
	}

	// in use twice: the second failure is the one returned
	write(t, bin, "old rtk")
	run(2, cargoInUseOut)
	if err := runCargoRTK(cargo, bin, time.Minute); err == nil || !strings.Contains(err.Error(), "rtk is in use") {
		t.Fatalf("in use twice: %v", err)
	}

	// another failure isn't retried
	write(t, bin, "old rtk")
	run(1, cargoDlltoolOut)
	if err := runCargoRTK(cargo, bin, time.Minute); err == nil || exists(aside) {
		t.Fatalf("dlltool: %v", err)
	}

	onWindows(t, false)
	run(1, cargoInUseOut)
	if err := runCargoRTK(cargo, bin, time.Minute); err == nil {
		t.Fatal("not Windows: retried")
	}
}

// cargo's own build folder is uni-switch's, so the retry builds nothing again
// and no build is left behind in the user's temp folder.
func TestRTKCargoTargetDir(t *testing.T) {
	sandbox(t)
	var dir string
	p := runPlain
	runPlain = func(c []string, _ time.Duration, env ...string) (string, error) {
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "CARGO_TARGET_DIR="); ok {
				dir = v
			}
		}
		return "", nil
	}
	t.Cleanup(func() { runPlain = p })
	if err := runCargoRTK([]string{"cargo"}, filepath.Join(t.TempDir(), "rtk"), time.Minute); err != nil || dir == "" {
		t.Fatalf("%v, CARGO_TARGET_DIR %q", err, dir)
	}
	if exists(dir) {
		t.Fatalf("%s left behind", dir)
	}
}

// A failed cargo says its first error, the cause, with what to do — not
// its last lines, which only say the build failed and where its files are.
func TestRTKCargoSaysWhy(t *testing.T) {
	cargoAt := []string{`C:\Users\me\.cargo\bin\cargo.exe`, "install"}
	for _, c := range []struct {
		out  string
		want []string
	}{
		{cargoInUseOut, []string{"cargo failed (exit code 101)", "rtk is in use", "failed to move", "(os error 5)"}},
		{cargoDlltoolOut, []string{"cargo failed", "missing a tool", "error calling dlltool 'dlltool.exe': program not found"}},
		{"error: linker `link.exe` not found\r\n  |\r\n  = note: program not found\r\n", []string{"Visual Studio Build Tools", "link.exe"}},
		{"error: failed to clone into: C:\\x\r\n\r\nCaused by:\r\n  network failure seems to have happened\r\n", []string{"couldn't download", "network failure"}},
	} {
		msg := installFailed(cargoAt, c.out, 101, errors.New("exit status 101")).Error()
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%q\nmissing %q", msg, w)
			}
		}
		if strings.Contains(msg, `C:\Users\me\.cargo\bin\cargo.exe failed`) || strings.Contains(msg, "CARGO_BUILD_BUILD_DIR") {
			t.Errorf("%q", msg)
		}
	}
}
