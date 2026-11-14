package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// #2096: with Settings' AutoRestart on, a downloaded update is restarted
// into by itself, but only once the gateway has nothing in flight; off, or
// with automatic updates off, it waits for a click as before.
func TestAutoRestartWaitsForIdle(t *testing.T) {
	busy := &fakeBusy{}
	busy.set(gateway.Busy{Requests: 1})
	u := &updater{state: "ready", latest: &update.Release{Version: "0.1.999"}}
	quick(t, busy, u)
	var restarts atomic.Int32
	u.autoRestart = func() bool { restarts.Add(1); return true }

	for _, s := range []settings.Settings{{}, {AutoRestart: true, NoAutoUpdate: true}} {
		u.autoTick(s)
		if w, _ := u.waitingFor(); w {
			t.Fatalf("%+v: a restart waits", s)
		}
	}
	on := settings.Settings{AutoRestart: true}
	u.autoTick(on)
	if w, _ := u.waitingFor(); !w {
		t.Fatal("AutoRestart on, update ready: no restart waits")
	}
	time.Sleep(150 * time.Millisecond)
	if restarts.Load() != 0 {
		t.Fatal("restarted with a request in flight")
	}
	u.autoTick(on) // a tick while waiting doesn't start a second wait
	busy.set(gateway.Busy{Last: time.Now()})
	eventually(t, "never restarted once idle", func() bool { return restarts.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := restarts.Load(); n != 1 {
		t.Fatalf("restarted %d times", n)
	}
}

// Nothing downloaded, nothing to restart into: no wait.
func TestAutoRestartNeedsAReadyUpdate(t *testing.T) {
	busy := &fakeBusy{}
	for _, state := range []string{"latest", "downloading", "checking", "error", "available"} {
		u := &updater{state: state, latest: &update.Release{Version: "0.1.999"}}
		quick(t, busy, u)
		u.autoRestart = func() bool { t.Errorf("%s: restarted", state); return true }
		u.autoTick(settings.Settings{AutoRestart: true})
		if w, _ := u.waitingFor(); w {
			t.Errorf("%s: a restart waits", state)
		}
	}
}

// The update card's Cancel calls the restart off for that version: the
// next tick doesn't wait for it again, a newer one is restarted into.
func TestAutoRestartCalledOff(t *testing.T) {
	busy := &fakeBusy{}
	busy.set(gateway.Busy{Requests: 1})
	old := updates
	updates = &updater{state: "ready", latest: &update.Release{Version: "0.1.999"}}
	t.Cleanup(func() { updates = old })
	quick(t, busy, updates)
	var restarts atomic.Int32
	updates.autoRestart = func() bool { restarts.Add(1); return true }
	on := settings.Settings{AutoRestart: true}
	updates.autoTick(on)
	mux := http.NewServeMux()
	updateRoutes(mux, &quitCount{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/update/install", strings.NewReader(`{"when":"cancel"}`)))
	var j updateJSON
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &j) != nil || j.Waiting {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	busy.set(gateway.Busy{})
	updates.autoTick(on)
	if w, _ := updates.waitingFor(); w {
		t.Fatal("waits again for the version called off")
	}
	updates.mu.Lock()
	updates.latest = &update.Release{Version: "0.1.1000"}
	updates.mu.Unlock()
	updates.autoTick(on)
	eventually(t, "a newer version wasn't restarted into", func() bool { return restarts.Load() == 1 })
}
