package gui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// A version downloaded and waiting for a restart gives way to a newer one
// out since, so the restart goes straight to the latest.
func TestUpdateStagedGivesWayToNewer(t *testing.T) {
	body := []byte("new uni-switch")
	sum := sha256.Sum256(body)
	var latest atomic.Value
	latest.Store("0.1.11")
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			downloads.Add(1)
			w.Write(body)
			return
		}
		json.NewEncoder(w).Encode(update.Release{Version: latest.Load().(string), Assets: map[string]update.Asset{
			update.BinaryAsset(): {URL: "http://" + r.Host + "/asset", SHA256: hex.EncodeToString(sum[:])},
		}})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	old := Version
	Version = "0.1.10"
	defer func() { Version = old }()
	exe, err := update.Executable()
	if err != nil {
		t.Fatal(err)
	}
	u := &updater{exe: exe}
	u.self, _ = os.Stat(exe)
	defer os.Remove(exe + ".new")

	u.check()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.11" || downloads.Load() != 1 {
		t.Fatalf("%+v, %d downloads", j, downloads.Load())
	}
	// nothing newer: it stays as it is, not downloaded again
	u.check()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.11" || downloads.Load() != 1 {
		t.Fatalf("%+v, %d downloads", j, downloads.Load())
	}
	// the feed down: still ready to restart into what it has
	t.Setenv("UNI_SWITCH_UPDATE_FEED", "http://127.0.0.1:1/")
	u.check()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.11" {
		t.Fatalf("%+v", j)
	}
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	// 0.1.13 out: that is the one it restarts into
	latest.Store("0.1.13")
	u.recheck(false)
	for u.json().State == "downloading" || u.json().State == "checking" {
		time.Sleep(10 * time.Millisecond)
	}
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.13" || downloads.Load() != 2 || u.staged == "" {
		t.Fatalf("%+v, %d downloads", j, downloads.Load())
	}
}
