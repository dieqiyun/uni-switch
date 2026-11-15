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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// coeo91 on Discord: "更新行为不对，他会在点击更新按钮后下载上一次或最近一次获取到的
// 最新版本，而不是实际的最新版本". A click that updates acts on what the feed
// says at the click, not on what the last check found: these tests run a
// check that finds 0.1.11, publish 0.1.12, and click.

// freshFeed is a fake update feed naming latest, whose asset counts its
// downloads. hold, while set, keeps the feed's answers back until it is
// closed.
type freshFeed struct {
	srv       *httptest.Server
	latest    atomic.Value
	downloads atomic.Int32
	mu        sync.Mutex
	hold      chan struct{}
}

func newFreshFeed(t *testing.T) *freshFeed {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	f := &freshFeed{}
	f.latest.Store("0.1.11")
	body := []byte("new uni-switch")
	sum := sha256.Sum256(body)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			f.downloads.Add(1)
			w.Write(body)
			return
		}
		f.mu.Lock()
		hold := f.hold
		f.mu.Unlock()
		if hold != nil {
			<-hold
		}
		v := f.latest.Load().(string)
		json.NewEncoder(w).Encode(update.Release{Version: v, URL: "https://example.invalid/v" + v, Assets: map[string]update.Asset{
			update.BinaryAsset(): {URL: "http://" + r.Host + "/asset", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])},
		}})
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv("UNI_SWITCH_UPDATE_FEED", f.srv.URL)
	old := Version
	Version = "0.1.10"
	t.Cleanup(func() { Version = old })
	return f
}

// freshUpdater is updates for the test: replacing the test's binary when
// exe is set, and never restarting (a handover stands in as under way).
func freshUpdater(t *testing.T, exe bool) *updater {
	t.Helper()
	u := &updater{}
	if exe {
		p, err := update.Executable()
		if err != nil {
			t.Fatal(err)
		}
		u.exe = p
		u.self, _ = os.Stat(p)
		t.Cleanup(func() { os.Remove(p + ".new") })
	}
	old := updates
	updates = u
	t.Cleanup(func() { updates = old })
	if pending.Load() == nil {
		pending.Store(&handover{})
		t.Cleanup(func() { pending.Store(nil) })
	}
	return u
}

// settled waits for u's check or download to end.
func settled(t *testing.T, u *updater) updateJSON {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if !u.asking() {
			if j := u.json(); j.State != "downloading" {
				return j
			}
		}
	}
	t.Fatalf("still %+v", u.json())
	return updateJSON{}
}

type openedURLs struct {
	quitCount
	mu   sync.Mutex
	urls []string
}

func (o *openedURLs) OpenURL(u string) error {
	o.mu.Lock()
	o.urls = append(o.urls, u)
	o.mu.Unlock()
	return nil
}

func postInstall(t *testing.T, w Windows) updateJSON {
	t.Helper()
	mux := http.NewServeMux()
	updateRoutes(mux, w)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/update/install", strings.NewReader("{}")))
	var j updateJSON
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &j) != nil {
		t.Fatalf("install: %d %s", rec.Code, rec.Body)
	}
	return j
}

// With Settings' Download updates automatically off, 0.1.11 downloaded on
// a click and 0.1.12 out since: Restart to update downloads 0.1.12 and
// restarts into it, rather than into 0.1.11.
func TestRestartClickTakesTheNewest(t *testing.T) {
	f := newFreshFeed(t)
	s := settings.Load()
	s.NoAutoDownload = true
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	u := freshUpdater(t, true)
	u.check()
	if j := u.json(); j.State != "found" || j.Latest != "0.1.11" {
		t.Fatalf("the check: %+v", j)
	}
	u.download()
	if j := settled(t, u); j.State != "ready" || j.Latest != "0.1.11" || f.downloads.Load() != 1 {
		t.Fatalf("Download update: %+v, %d downloads", j, f.downloads.Load())
	}
	f.latest.Store("0.1.12")
	j := postInstall(t, &quitCount{})
	if j.State == "ready" && j.Latest == "0.1.11" {
		t.Fatalf("Restart to update went into the version the last check found: %+v", j)
	}
	if j := settled(t, u); j.State != "ready" || j.Latest != "0.1.12" || f.downloads.Load() != 2 {
		t.Fatalf("after the click: %+v, %d downloads", j, f.downloads.Load())
	}
	// nothing newer: the click restarts into what is downloaded, not
	// downloading it again
	if j := postInstall(t, &quitCount{}); j.State != "ready" || j.Latest != "0.1.12" || f.downloads.Load() != 2 {
		t.Fatalf("the newest downloaded: %+v, %d downloads", j, f.downloads.Load())
	}
}

// A feed slower than the restart's wait for it: the restart doesn't go
// into the download it has, which the answer may have made old; it says
// it is checking, and the answer, once in, is downloaded.
func TestRestartWaitsForTheFeed(t *testing.T) {
	f := newFreshFeed(t)
	u := freshUpdater(t, true)
	oldTicks, oldTick := recheckTicks, recheckTick
	recheckTicks, recheckTick = 3, time.Millisecond
	t.Cleanup(func() { recheckTicks, recheckTick = oldTicks, oldTick })
	u.check()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.11" {
		t.Fatalf("the check: %+v", j)
	}
	f.latest.Store("0.1.12")
	hold := make(chan struct{})
	f.mu.Lock()
	f.hold = hold
	f.mu.Unlock()
	j := postInstall(t, &quitCount{})
	f.mu.Lock()
	f.hold = nil
	f.mu.Unlock()
	close(hold)
	if j.State != "checking" {
		t.Fatalf("restarted before the feed answered: %+v", j)
	}
	if j := settled(t, u); j.State != "ready" || j.Latest != "0.1.12" || f.downloads.Load() != 2 {
		t.Fatalf("the answer: %+v, %d downloads", j, f.downloads.Load())
	}
}

// The tray's Restart to update asks the feed as the page's does: with
// 0.1.12 out since, it is downloaded and restarted into, once.
func TestTrayRestartTakesTheNewest(t *testing.T) {
	busy := &fakeBusy{}
	u := freshUpdater(t, true)
	quick(t, busy, u)
	waitRecheck = (*updater).recheck
	f := newFreshFeed(t)
	u.check()
	if j := u.json(); j.State != "ready" || j.Latest != "0.1.11" {
		t.Fatalf("the check: %+v", j)
	}
	f.latest.Store("0.1.12")
	into := make(chan string, 4)
	u.restartAsked(func() bool {
		u.mu.Lock()
		into <- u.latest.Version
		u.mu.Unlock()
		return true
	})
	select {
	case v := <-into:
		if v != "0.1.12" {
			t.Fatalf("the tray restarted into %s with 0.1.12 out", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("never restarted: %+v", u.json())
	}
	if n := f.downloads.Load(); n != 2 {
		t.Fatalf("%d downloads", n)
	}
	// the gateway busy: the restart waits, as before (#577)
	busy.set(gateway.Busy{Requests: 1})
	u.restartAsked(func() bool { into <- "busy"; return true })
	if w, _ := u.waitingFor(); !w {
		t.Fatal("restarted with a request in flight")
	}
}

// A uni-switch that can't replace itself sends a click to the release page:
// the newest's, not the one an old check found.
func TestAvailableOpensTheNewestPage(t *testing.T) {
	f := newFreshFeed(t)
	u := freshUpdater(t, false)
	u.check()
	if j := u.json(); j.State != "available" || j.Latest != "0.1.11" {
		t.Fatalf("the check: %+v", j)
	}
	f.latest.Store("0.1.12")
	w := &openedURLs{}
	// a check a moment ago: the page it found is opened, the feed not asked
	postInstall(t, w)
	u.mu.Lock()
	u.asked = time.Now().Add(-2 * time.Minute)
	u.mu.Unlock()
	postInstall(t, w)
	w.mu.Lock()
	defer w.mu.Unlock()
	if want := []string{"https://example.invalid/v0.1.11", "https://example.invalid/v0.1.12"}; strings.Join(w.urls, " ") != strings.Join(want, " ") {
		t.Fatalf("opened %q, want %q", w.urls, want)
	}
}
