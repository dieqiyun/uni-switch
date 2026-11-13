package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/fx"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The Settings page's currency row, and any cost shown before it's ever
// opened, both need the dollar-to-yuan rate: state() and settingsState()
// both carry it (#212). state() asks for it only once cny is actually
// chosen — usd never touches the network for a rate nothing shows with.
func TestFXInState(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	fx.Reset()

	// pre-seed the fx cache so nothing here ever reaches the network
	cache := fx.CachePath()
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	b, err := json.Marshal(fx.Rate{CNYPerUSD: 7.25, At: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, b, 0o644); err != nil {
		t.Fatal(err)
	}
	fx.Reset()

	if err := settings.Save(settings.Settings{Currency: "usd"}); err != nil {
		t.Fatal(err)
	}
	if got := state().FX; got.Rate != 0 {
		t.Fatalf("usd: state().FX = %+v, want no rate fetched", got)
	}
	if got := settingsState().FX; got.Rate != 7.25 || got.Stale {
		t.Fatalf("settingsState always carries the rate: %+v", got)
	}

	if err := settings.Save(settings.Settings{Currency: "cny"}); err != nil {
		t.Fatal(err)
	}
	fx.Reset()
	if got := state().FX; got.Rate != 7.25 || got.At == nil || got.Stale {
		t.Fatalf("cny: state().FX = %+v", got)
	}
}

// The state never waits on the rate's endpoint (#541): with yesterday's
// rate cached and the endpoint not answering, it answers with
// yesterday's, and with the new one once that is in. The endpoint
// answers only once the first look is back, so that look coming back
// at all is the proof, not how long it took on a busy machine.
func TestFXStateDoesNotWait(t *testing.T) {
	sandboxHome(t)
	fx.Reset()
	release := make(chan struct{})
	var once sync.Once
	let := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(`{"result":"success","rates":{"CNY":7.31}}`))
	}))
	t.Cleanup(func() {
		let()
		fx.Settle()
		srv.Close()
		fx.Reset()
	})
	t.Cleanup(fx.SetRateURL(srv.URL))
	cache := fx.CachePath()
	os.MkdirAll(filepath.Dir(cache), 0o755)
	b, _ := json.Marshal(fx.Rate{CNYPerUSD: 7.25, At: time.Now().Add(-24 * time.Hour)})
	if err := os.WriteFile(cache, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(settings.Settings{Currency: "cny"}); err != nil {
		t.Fatal(err)
	}
	first := make(chan fxJSON, 1)
	go func() { first <- state().FX }()
	var got fxJSON
	select {
	case got = <-first:
	case <-time.After(time.Minute):
		t.Fatal("the state waited on the rate's endpoint")
	}
	let()
	if got.Rate != 7.25 || !got.Stale {
		t.Fatalf("first look: %+v, want yesterday's 7.25, stale", got)
	}
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(100 * time.Millisecond) {
		if got = state().FX; got.Rate == 7.31 && !got.Stale {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the new rate never came in: %+v", got)
		}
	}
}
