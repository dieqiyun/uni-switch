package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// What brew info rtk printed on an Apple silicon Mac (macOS 27), and with
// HOMEBREW_FAKE_MACOS=14.7, where Homebrew has no rtk bottle: read from a
// real brew 5 on mote.
const (
	brewInfoBottled = `==> rtk: stable 1.1.0 (bottled), HEAD
CLI proxy to minimize LLM token consumption
https://www.rtk-ai.app/
Not installed
From: https://github.com/Homebrew/homebrew-core/blob/HEAD/Formula/r/rtk.rb
License: Apache-2.0
==> Dependencies
Build (1): rust
`
	brewInfoSource = `==> rtk: stable 1.1.0, HEAD
CLI proxy to minimize LLM token consumption
https://www.rtk-ai.app/
Not installed
From: https://github.com/Homebrew/homebrew-core/blob/HEAD/Formula/r/rtk.rb
License: Apache-2.0
==> Dependencies
Build (1): rust
`
)

// fakeBrew puts a brew in the sandbox's bin answering info rtk with info
// (and exit code), counting each ask in a file; install rtk prints
// install and fails. The kept answer is dropped before and after.
func fakeBrew(t *testing.T, h, info string, code int, install string) (asked func() int) {
	t.Helper()
	bin := filepath.Join(h, "bin")
	write(t, filepath.Join(h, "info.txt"), info)
	write(t, filepath.Join(h, "install.txt"), install)
	count := filepath.Join(h, "asked")
	testenv.Program(t, filepath.Join(bin, "brew"), `#!/bin/sh
case "$*" in
"info rtk") echo x >> "`+count+`"; /bin/cat "`+filepath.Join(h, "info.txt")+`"; exit `+itoa(code)+` ;;
"install rtk") /bin/cat "`+filepath.Join(h, "install.txt")+`"; exit 1 ;;
esac
exit 2
`)
	testenv.Program(t, filepath.Join(bin, "curl"), "#!/bin/sh\nexit 1\n")
	forgetBottled := func() {
		bottled.Lock()
		bottled.brew, bottled.ok, bottled.at = "", false, time.Time{}
		bottled.Unlock()
	}
	forgetBottled()
	t.Cleanup(forgetBottled)
	proc.Forget()
	t.Cleanup(proc.Forget)
	return func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "x")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}

// Scott__D on X: Install RTK ran brew install rtk on a Mac Homebrew has no
// rtk bottle for, so brew set out to build rust, llvm and python from
// source, and the five-minute wait ended it every time. Such a brew is
// passed over for rtk's own script; one with the bottle is still used.
func TestRTKInstallsWithBrewOnlyFromItsBottle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("winget installs rtk on Windows; the fakes are shell scripts")
	}
	script := "curl -fsSL " + rtkScript + " | sh"
	for _, c := range []struct {
		name, info string
		code       int
		want       string
	}{
		{"bottled", brewInfoBottled, 0, "brew install rtk"},
		{"built from source", brewInfoSource, 0, script},
		// mote's brew, which can't run git: it says nothing of rtk
		{"brew that fails", "Error: Unable to automatically install a working Git.\n", 1, script},
		{"brew that says nothing", "", 0, script},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := sandbox(t)
			asked := fakeBrew(t, h, c.info, c.code, "")
			if v := ReadRTK(); v.Path != "" || v.Install != c.want {
				t.Fatalf("install %q, want %q (path %q)", v.Install, c.want, v.Path)
			}
			// the page reads it again: brew is asked once
			if v := ReadRTK(); v.Install != c.want || asked() != 1 {
				t.Fatalf("read again: install %q, brew asked %d times", v.Install, asked())
			}
		})
	}
}

// A brew info that doesn't answer is given brewInfoWait, then rtk's own
// script is used.
func TestRTKBrewInfoThatHangs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	fakeBrew(t, h, "", 0, "")
	testenv.Program(t, filepath.Join(h, "bin", "brew"), "#!/bin/sh\nexec /bin/sleep 60\n")
	w := brewInfoWait
	brewInfoWait = 300 * time.Millisecond
	t.Cleanup(func() { brewInfoWait = w })
	start := time.Now()
	if v := ReadRTK(); !strings.HasPrefix(v.Install, "curl ") {
		t.Fatalf("install %q", v.Install)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("waited %s for brew", d)
	}
}

// A failed brew install says Homebrew's error and what to do, not the
// support-tier notice its output ends with. The output is brew 5's on mote
// (macOS without the Command Line Tools for its Homebrew), from its last
// download on.
func TestRTKBrewInstallSaysBrewsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fakes are shell scripts")
	}
	h := sandbox(t)
	fakeBrew(t, h, brewInfoBottled, 0, "✔︎ Formula rtk (1.1.0)\n✔︎ Formula rust (1.99.1)\n"+
		"Error: No developer tools installed.\n\nInstall the Command Line Tools:\n  xcode-select --install\n"+
		"Error: Xcode alone is not sufficient on Sonoma.\n\nInstall the Command Line Tools:\n  xcode-select --install\n\n"+
		"This is a Tier 3 configuration:\n  https://docs.brew.sh/Support-Tiers#tier-3\n"+
		"You can report issues with Tier 3 configurations to Homebrew/* repositories!\n  \n"+
		"Read the above document before opening any issues or PRs.\n")
	_, err := InstallRTK()
	if err == nil {
		t.Fatal("brew failed, and InstallRTK didn't")
	}
	want := "brew failed (exit code 1) — Error: No developer tools installed. Install the Command Line Tools: xcode-select --install"
	if err.Error() != want {
		t.Fatalf("error %q\nwant  %q", err, want)
	}
}
