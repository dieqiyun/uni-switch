package gui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// #2296 (meifu2027): on a metered network or a proxy billed by traffic,
// a newer version found was downloaded at once, by the clock and by
// Settings' Check alike. With Settings' NoAutoDownload, a check only says
// it is out, with its notes and the size of the file; a click on Download
// update downloads it, and nothing else does. Off, as by default, a check
// downloads as before.
func TestUpdateDownloadWaitsForAClick(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))

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
		json.NewEncoder(w).Encode(update.Release{Version: latest.Load().(string), Notes: "- faster", Assets: map[string]update.Asset{
			update.BinaryAsset(): {URL: "http://" + r.Host + "/asset", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])},
		}})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	oldV := Version
	Version = "0.1.10"
	defer func() { Version = oldV }()
	exe, err := update.Executable()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(exe + ".new")

	s := settings.Load()
	s.NoAutoDownload = true
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	o := updates
	u := &updater{exe: exe}
	u.self, _ = os.Stat(exe)
	updates = u
	defer func() { updates = o }()
	mux := http.NewServeMux()
	updateRoutes(mux, &quitCount{})
	post := func(path, in string) updateJSON {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(in)))
		var j updateJSON
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &j) != nil {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return j
	}
	settle := func() updateJSON {
		t.Helper()
		for range 500 {
			if j := u.json(); j.State != "checking" && j.State != "downloading" {
				return j
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("still %s", u.json().State)
		return updateJSON{}
	}

	// Settings' Check: said to be out, with its notes and size, not downloaded
	if j := post("/api/update/check", "{}"); j.State != "found" || j.Latest != "0.1.11" || j.Notes == "" || j.Size != int64(len(body)) || downloads.Load() != 0 {
		t.Fatalf("Check: %+v, %d downloads", j, downloads.Load())
	}
	// the clock's check: the same
	u.checkByClock()
	if j := u.json(); j.State != "found" || downloads.Load() != 0 {
		t.Fatalf("the clock's check: %+v, %d downloads", j, downloads.Load())
	}
	// Download update, clicked: downloaded, ready to restart into
	post("/api/update/install", "{}")
	if j := settle(); j.State != "ready" || j.Latest != "0.1.11" || downloads.Load() != 1 {
		t.Fatalf("Download update: %+v, %d downloads", j, downloads.Load())
	}
	// a newer one out since: the one downloaded stays, the newer isn't fetched
	latest.Store("0.1.12")
	u.checkByClock()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.11" || downloads.Load() != 1 {
		t.Fatalf("a newer one, downloads held: %+v, %d downloads", j, downloads.Load())
	}

	// a save from a page that doesn't send the switch leaves it as it is
	rec := httptest.NewRecorder()
	Handler(nil, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"theme":"dark"}`)))
	if rec.Code != 200 || !settings.Load().NoAutoDownload {
		t.Fatalf("a save without noAutoDownload turned it off: %d %s", rec.Code, rec.Body)
	}

	// off again, the default: a check downloads what it finds
	s = settings.Load()
	s.NoAutoDownload = false
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	u.check()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.12" || downloads.Load() != 2 {
		t.Fatalf("downloads on: %+v, %d downloads", j, downloads.Load())
	}
}
