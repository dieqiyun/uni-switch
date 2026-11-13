package gui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// #894: on Windows with Smart App Control on, the new unsigned exe was
// put in and refused at the restart, and uni-switch never started again. A
// restart into a version that doesn't start leaves this one running and
// says why; the clock's checks don't download that version again, and
// a check asked for tries it once more.
func TestUpdateThatWontStart(t *testing.T) {
	body := []byte("new uni-switch")
	sum := sha256.Sum256(body)
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			downloads.Add(1)
			w.Write(body)
			return
		}
		json.NewEncoder(w).Encode(update.Release{Version: "0.1.11", Assets: map[string]update.Asset{
			update.BinaryAsset(): {URL: "http://" + r.Host + "/asset", SHA256: hex.EncodeToString(sum[:])},
		}})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	oldV := Version
	Version = "0.1.10"
	defer func() { Version = oldV }()
	t.Cleanup(update.ClearBlocked)

	// the running binary, in a folder of its own
	exe := filepath.Join(t.TempDir(), "uni-switch")
	os.WriteFile(exe, []byte("running uni-switch"), 0o755)
	self, _ := os.Stat(exe)
	staged := exe + ".new"
	os.WriteFile(staged, body, 0o755)

	o := updates
	u := &updater{exe: exe, self: self, staged: staged, state: "ready", latest: &update.Release{Version: "0.1.11"}}
	updates = u
	defer func() { updates = o }()
	var relaunched atomic.Int32
	wasRelaunch := relaunchBinary
	relaunchBinary = func(exe, old string, window bool, view string) error {
		relaunched.Add(1)
		// what update.RelaunchBinary does when Windows refuses the new exe:
		// the running one put back, and says so
		os.WriteFile(exe, []byte("running uni-switch"), 0o755)
		return &update.BlockedError{Err: &os.PathError{Op: "fork/exec", Path: exe, Err: syscall.Errno(4551)}}
	}
	defer func() { relaunchBinary = wasRelaunch }()

	if restartToUpdate(false, true, "settings") {
		t.Fatal("restartToUpdate said to quit, with nothing left that starts")
	}
	if relaunched.Load() != 1 {
		t.Fatalf("relaunched %d times", relaunched.Load())
	}
	j := u.json()
	if j.State != "blocked" || j.Blocked == nil || j.Blocked.Version != "0.1.11" || !j.Blocked.Policy {
		t.Fatalf("after the refused restart: %+v", j)
	}
	if _, err := os.Stat(filepath.Join(settings.Dir(), updatedInAppFile)); !os.IsNotExist(err) {
		t.Errorf("noted as updated in the app: %v", err)
	}

	// uni-switch starts again later (the note is on disk): the clock's check
	// leaves the version alone
	u2 := &updater{exe: exe}
	u2.self, _ = os.Stat(exe)
	u2.blocked = update.ReadBlocked(Version)
	if u2.blocked == nil {
		t.Fatal("the refused version isn't noted")
	}
	u2.checkByClock()
	if j := u2.json(); j.State != "blocked" || j.Blocked == nil || downloads.Load() != 0 {
		t.Fatalf("clock's check: %+v, %d downloads", j, downloads.Load())
	}
	// Check, clicked: it is downloaded to be tried again
	if self, err := update.Executable(); err == nil {
		defer os.Remove(self + ".new") // where StageBinary puts it
	}
	u2.check()
	if j := u2.json(); j.State != "ready" || downloads.Load() != 1 {
		t.Fatalf("clicked check: %+v, %d downloads", j, downloads.Load())
	}
	// once this version is the one running, the note goes
	if update.ReadBlocked("0.1.11") != nil {
		t.Error("the note outlived the version")
	}
}
