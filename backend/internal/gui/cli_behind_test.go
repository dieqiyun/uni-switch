package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// The window's advice about a copied `uni-switch` command is a bare Lstat — no
// version read, because `uni-switch version` isn't read-only: the old binary
// runs its old migrations over the user's real settings first. Only
// `uni-switch update`, which the user started, reads versions. The advice can
// be dismissed, and the dismiss is kept per version: the next app update
// asks once more.
func TestCLIBehindIsLstatOnlyAndDismissible(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only the Mac keeps the command apart from the app")
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	bin := t.TempDir()
	t.Setenv("UNI_SWITCH_BIN_DIR", bin)
	wasV, wasOnce, wasVal := Version, cliBehindOnce, cliBehindVal
	t.Cleanup(func() { Version, cliBehindOnce, cliBehindVal = wasV, wasOnce, wasVal })
	Version = "0.1.500"
	cliBehindOnce = new(sync.Once) // another test may have asked already
	cliBehindVal = ""

	// a copy: the window says so without running anything (nothing here is
	// executable — a binary would have to be run to report a version)
	cli := filepath.Join(bin, "uni-switch")
	if err := os.WriteFile(cli, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cliBehind(); got == "" {
		t.Fatal("cliBehind = empty, want the copy's path: a copied command can't follow updates")
	}

	// the link the installer makes: quiet
	mux := http.NewServeMux()
	cliBehindRoutes(mux)
	post := func() int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/cli-behind/quiet", nil))
		return rec.Code
	}
	if c := post(); c != http.StatusNoContent {
		t.Fatalf("dismiss: %d", c)
	}
	if got := cliBehind(); got != "" {
		t.Fatalf("cliBehind = %q after dismiss, want empty", got)
	}
	b, err := os.ReadFile(filepath.Join(home, "uni-switch", cliQuietFile))
	if err != nil || string(b) != Version+"\n" {
		t.Fatalf("dismiss file = %q, %v; want this version, so the next one asks again", b, err)
	}

	// the dismiss outlives a restart: the copy is still there, but the
	// quiet file still names this version, so a fresh run stays quiet
	cliBehindOnce = new(sync.Once)
	cliBehindVal = ""
	if got := cliBehind(); got != "" {
		t.Fatalf("cliBehind = %q after a restart, want empty: the dismiss is kept per version", got)
	}

	// the next version asks once more
	Version = "0.1.501"
	cliBehindOnce = new(sync.Once)
	cliBehindVal = ""
	if got := cliBehind(); got != cli {
		t.Fatalf("cliBehind = %q on the next version, want %q: the dismiss only covers the version it was made at", got, tilde(cli))
	}

	// a link, not a copy: quiet even undismissed
	os.Remove(cli)
	if err := os.Symlink("/Applications/uni-switch.app/Contents/MacOS/uni-switch", cli); err != nil {
		t.Fatal(err)
	}
	cliBehindOnce = new(sync.Once)
	cliBehindVal = ""
	if got := cliBehind(); got != "" {
		t.Fatalf("cliBehind = %q for the installer's link, want empty", got)
	}
}
