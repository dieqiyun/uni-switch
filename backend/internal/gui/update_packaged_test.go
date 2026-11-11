package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// The AUR's uni-switch-bin installs /usr/bin/uni-switch, and with pkexec there
// uni-switch put its own download over the package's file on a restart, so
// pacman's database named one version and the disk had another, and the
// next yay upgrade wrote over it again (shuhai on Discord). A binary a
// package manager installed now says a version is out and the command
// that updates it, and never downloads or replaces anything, even where
// it may write or ask for the password.
func TestUpdatePackagedLeavesItToTheManager(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			t.Errorf("a packaged binary downloaded the release")
		}
		json.NewEncoder(w).Encode(update.Release{Version: "0.1.11", URL: "https://github.com/dieqiyun/uni-switch/releases/tag/v0.1.11", Assets: map[string]update.Asset{
			update.BinaryAsset(): {URL: "http://" + r.Host + "/asset"},
		}})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	old := Version
	Version = "0.1.10"
	defer func() { Version = old }()
	wasE, wasP := canElevate, packagedBy
	defer func() { canElevate, packagedBy = wasE, wasP }()
	canElevate = func() bool { return true }

	dir := t.TempDir() // writable: ownership alone decides
	exe := filepath.Join(dir, "uni-switch")
	if err := os.WriteFile(exe, []byte("uni-switch"), 0o755); err != nil {
		t.Fatal(err)
	}
	packagedBy = func(string) *update.Packaged { return &update.Packaged{Manager: "pacman", Package: "uni-switch-bin"} }
	u := &updater{}
	u.placeExe(exe)
	if u.exe != "" {
		t.Fatalf("a packaged binary is to be replaced: %q", u.exe)
	}
	u.check()
	j := u.json()
	if j.State != "available" || j.Stuck != "package" || j.Packaged == nil || j.Packaged.Manager != "pacman" || j.Packaged.Package != "uni-switch-bin" || j.Packaged.Command != "yay -S uni-switch-bin" || j.Latest != "0.1.11" {
		t.Fatalf("%+v %+v", j, j.Packaged)
	}
	if u.install(true) {
		t.Fatal("install put something in")
	}
	if b, _ := os.ReadFile(exe); string(b) != "uni-switch" {
		t.Fatalf("the package's file was changed: %q", b)
	}

	// a binary no package owns updates itself as before
	packagedBy = func(string) *update.Packaged { return nil }
	u = &updater{}
	u.placeExe(exe)
	if u.exe != exe || u.stuck != "" {
		t.Fatalf("not packaged: exe %q, stuck %q", u.exe, u.stuck)
	}
}
