package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withCLIVersion points cliVersion at a fixed answer for one test, so the
// version decision is exercised without building or running a uni-switch.
func withCLIVersion(t *testing.T, v string) {
	t.Helper()
	was := cliVersion
	cliVersion = func(string) string { return v }
	t.Cleanup(func() { cliVersion = was })
}

// StaleCLI says only what deserves saying: a copied `uni-switch` command that
// is a release *behind* the app — the stale build behind #531's wedged
// sync. It stays quiet for the installer's link (already follows the app),
// an absent command, a copy at or ahead of the app, and a copy that isn't a
// release (a source build, or one that won't run): none of those is a stale
// release to nag about. The bin dir is pointed at a temp dir through
// UNI_SWITCH_BIN_DIR, the one install.sh honors, so the test needs no real
// $HOME; only the Mac has the two-places problem, so it skips elsewhere.
func TestStaleCLI(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only the Mac keeps the command apart from the app")
	}
	const app = "0.1.500"
	bin := t.TempDir()
	t.Setenv("UNI_SWITCH_BIN_DIR", bin)
	cli := filepath.Join(bin, "uni-switch")
	writeCopy := func() { os.WriteFile(cli, []byte("a build"), 0o755) }

	t.Run("a copied release behind the app is stale", func(t *testing.T) {
		writeCopy()
		withCLIVersion(t, "0.1.462")
		if got := StaleCLI(app); got != cli {
			t.Errorf("StaleCLI = %q, want %q: an older copy won't follow the app", got, cli)
		}
	})

	t.Run("a copy current with the app is quiet", func(t *testing.T) {
		writeCopy()
		withCLIVersion(t, app)
		if got := StaleCLI(app); got != "" {
			t.Errorf("StaleCLI = %q, want empty: a copy at the app's version isn't behind", got)
		}
	})

	t.Run("a copy ahead of the app is quiet", func(t *testing.T) {
		writeCopy()
		withCLIVersion(t, "0.1.600")
		if got := StaleCLI(app); got != "" {
			t.Errorf("StaleCLI = %q, want empty: a newer copy leads, not lags", got)
		}
	})

	t.Run("a source build is quiet", func(t *testing.T) {
		writeCopy()
		withCLIVersion(t, "dev")
		if got := StaleCLI(app); got != "" {
			t.Errorf("StaleCLI = %q, want empty: the user built it, don't nag", got)
		}
	})

	t.Run("a copy that won't run is quiet", func(t *testing.T) {
		writeCopy()
		withCLIVersion(t, "")
		if got := StaleCLI(app); got != "" {
			t.Errorf("StaleCLI = %q, want empty: an unreadable copy can't run the stale command either", got)
		}
	})

	t.Run("the installer's link is quiet", func(t *testing.T) {
		os.Remove(cli)
		os.Symlink("/Applications/uni-switch.app/Contents/MacOS/uni-switch", cli)
		withCLIVersion(t, "0.1.000") // would look old if we dereferenced it
		if got := StaleCLI(app); got != "" {
			t.Errorf("StaleCLI = %q, want empty: a link already follows the app", got)
		}
	})

	t.Run("no command is quiet", func(t *testing.T) {
		os.Remove(cli)
		if got := StaleCLI(app); got != "" {
			t.Errorf("StaleCLI = %q, want empty: nothing installed, nothing to say", got)
		}
	})
}
