package testenv

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// MinBun is the oldest Bun uni-switch runs plugins on: plugin.BunVersion,
// which internal/plugin's TestMinBunIsBunVersion keeps it equal to (testenv
// can't import internal/plugin, whose own tests use it).
const MinBun = "1.4.2"

// bunRun is what running a bun once found: its version, "" when it didn't
// say one.
type bunRun struct {
	once    sync.Once
	version string
}

// bunsRun holds a *bunRun for each bun Bun has run.
var bunsRun sync.Map

// Bun is the bun on PATH for a test that runs plugins with it (as
// UNI_SWITCH_BUN), run once before the test opens its deadline; a machine
// without one skips the test. A bun just installed or upgraded hasn't run
// on this machine yet, and macOS checks a program before its first exec,
// which can take tens of seconds on a busy machine: that check must not eat
// the minute a test gives its plugin calls (ggbdpq, #1306).
//
// A bun older than MinBun skips the test too, saying so: uni-switch never runs
// plugins on one (it downloads MinBun or newer), and an older one fails
// these tests for its own differences, not uni-switch's. Bun 1.3 names the
// default port in a proxy's request line (GET http://models.invalid:80/list)
// and its node:http2 answers status 0, which failed the plugin proxy tests
// on a developer's machine (#1600).
func Bun(t testing.TB) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("no bun on PATH")
	}
	r, _ := bunsRun.LoadOrStore(bun, new(bunRun))
	run := r.(*bunRun)
	run.once.Do(func() {
		out, _ := exec.Command(bun, "--version").Output()
		run.version = strings.TrimSpace(string(out))
	})
	if older(run.version, MinBun) {
		t.Skipf("bun %s on PATH (%s) is older than %s, the oldest uni-switch runs plugins on; install %s or newer to run this test", run.version, bun, MinBun, MinBun)
	}
	return bun
}

// older is whether version v is before min, comparing major.minor.patch
// as numbers; a version that doesn't read as one is not older.
func older(v, min string) bool {
	a, ok := semver(v)
	if !ok {
		return false
	}
	b, ok := semver(min)
	if !ok {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func semver(v string) ([3]int, bool) {
	var n [3]int
	v, _, _ = strings.Cut(v, "-") // 1.4.3-canary.20261010.1
	v, _, _ = strings.Cut(v, "+")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}
