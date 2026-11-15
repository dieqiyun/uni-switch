package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// "Up to date" is about the moment the feed was asked. RickyWei's Version
// row said 已是最新 on v0.1.1402 with v0.1.1517 out: the answer of a check
// made while 1402 was the newest, never made again. A "latest" older than
// freshAfter is no longer said, and the Version row (fresh=1) asks again
// with automatic updates on; with them off it asks nothing by itself.
func TestStaleLatestIsNotUpToDate(t *testing.T) {
	sandboxHome(t)
	var newest atomic.Value
	newest.Store("0.1.1402")
	var asks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asks.Add(1)
		json.NewEncoder(w).Encode(update.Release{Version: newest.Load().(string)})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL)
	oldV := Version
	Version = "0.1.1402"
	defer func() { Version = oldV }()
	u := &updater{} // nothing it may replace: a newer one is "available"
	oldU := updates
	updates = u
	defer func() { updates = oldU }()
	mux := http.NewServeMux()
	updateRoutes(mux, nil)
	get := func(path string) updateJSON {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		var j updateJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
			t.Fatalf("%s: %v %s", path, err, rec.Body)
		}
		return j
	}
	age := func() {
		u.mu.Lock()
		u.asked = time.Now().Add(-freshAfter - time.Minute)
		u.mu.Unlock()
	}

	u.check()
	if j := get("/api/update?fresh=1&lang=zh"); j.State != "latest" {
		t.Fatalf("just checked: %+v", j)
	}
	if asks.Load() != 1 {
		t.Fatalf("a fresh latest asked the feed again: %d asks", asks.Load())
	}

	// a day on, 115 releases out, no check since
	newest.Store("0.1.1517")
	age()
	if j := get("/api/update?lang=zh"); j.State == "latest" {
		t.Fatalf("an old answer still says up to date: %+v", j)
	}
	if asks.Load() != 1 {
		t.Fatalf("a read other than the Version row's asked the feed: %d asks", asks.Load())
	}
	j := get("/api/update?fresh=1&lang=zh")
	for deadline := time.Now().Add(10 * time.Second); j.State == "checking" && time.Now().Before(deadline); j = get("/api/update?fresh=1&lang=zh") {
		time.Sleep(10 * time.Millisecond)
	}
	if j.State != "available" || j.Latest != "0.1.1517" {
		t.Fatalf("the Version row drawn over an old answer: %+v", j)
	}

	// automatic updates off: the old answer isn't said, and nothing is asked
	s := settings.Load()
	s.NoAutoUpdate = true
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	newest.Store("0.1.1402")
	Version = "0.1.1517"
	u.check()
	if j := get("/api/update?fresh=1"); j.State != "latest" {
		t.Fatalf("checked by a click: %+v", j)
	}
	age()
	n := asks.Load()
	if j := get("/api/update?fresh=1"); j.State != "" {
		t.Fatalf("auto updates off, old answer: %+v", j)
	}
	if asks.Load() != n {
		t.Fatal("asked the feed with automatic updates off")
	}
}
