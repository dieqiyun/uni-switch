package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// `uni-switch update` from the AUR's uni-switch-bin (/usr/bin/uni-switch) asked for the
// password and replaced the package's file under pacman (shuhai on
// Discord). It now says the version is out and the command that updates
// the package, and replaces nothing.
func TestUpdatePackagedSaysTheManager(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			t.Errorf("a packaged binary downloaded the release")
		}
		json.NewEncoder(w).Encode(update.Release{Version: "0.1.11", URL: "https://github.com/dieqiyun/uni-switch/releases/tag/v0.1.11",
			Assets: map[string]update.Asset{update.BinaryAsset(): {URL: "http://" + r.Host + "/asset"}}})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	exe := filepath.Join(t.TempDir(), "uni-switch")
	if err := os.WriteFile(exe, []byte("uni-switch"), 0o755); err != nil {
		t.Fatal(err)
	}
	wasV, wasX, wasE, wasP := version, updateExecutable, updateCanElevate, updatePackagedBy
	t.Cleanup(func() { version, updateExecutable, updateCanElevate, updatePackagedBy = wasV, wasX, wasE, wasP })
	version = "0.1.10"
	updateExecutable = func() (string, error) { return exe, nil }
	updateCanElevate = func() bool { return true }
	updatePackagedBy = func(string) *update.Packaged { return &update.Packaged{Manager: "pacman", Package: "uni-switch-bin"} }

	err := updateCmd([]string{"update"})
	if err == nil || !strings.Contains(err.Error(), "pacman") || !strings.Contains(err.Error(), "yay -S uni-switch-bin") {
		t.Fatalf("%v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "uni-switch" {
		t.Fatalf("the package's file was changed: %q", b)
	}
}
