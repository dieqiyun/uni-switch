package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func forgetLastProviders() {
	lastProviders.Lock()
	lastProviders.kept, lastProviders.read = nil, false
	lastProviders.Unlock()
}

// #1519 (fatkun): the Providers page draws the list kept from the last
// read at once (?last=1), this run's or the run before's, and the fresh
// one after; none kept is null. What can hold a secret (a header's value,
// a proxy's password, the raw key) never reaches the file.
func TestLastKnownProviders(t *testing.T) {
	sandboxHome(t)
	forgetLastProviders()
	t.Cleanup(forgetLastProviders)
	if _, err := provider.Add(provider.Provider{ID: "own", Name: "Own", Chat: "http://127.0.0.1:1/v1", Key: "sk-raw-key-1519",
		Models: []string{"m1"}, Headers: map[string]string{"X-Token": "hdr-secret-1519"}, Proxy: "http://u:proxy-secret-1519@127.0.0.1:2"}); err != nil {
		t.Fatal(err)
	}
	srv := Handler(nil, nil)
	get := func(path string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return strings.TrimSpace(rec.Body.String())
	}
	type list struct {
		Last      bool `json:"last"`
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	has := func(l list, id string) bool {
		for _, p := range l.Providers {
			if p.ID == id {
				return true
			}
		}
		return false
	}

	if b := get("/api/providers?last=1"); b != "null" {
		t.Fatalf("nothing kept yet: want null, got %.200s", b)
	}
	var fresh list
	json.Unmarshal([]byte(get("/api/providers")), &fresh)
	if fresh.Last || !has(fresh, "own") {
		t.Fatalf("fresh list: %+v", fresh)
	}
	file, err := os.ReadFile(lastProvidersPath())
	if err != nil {
		t.Fatalf("the read was not kept: %v", err)
	}
	for _, s := range []string{"sk-raw-key-1519", "hdr-secret-1519", "proxy-secret-1519"} {
		if strings.Contains(string(file), s) {
			t.Errorf("the kept list holds %s", s)
		}
	}
	if fi, err := os.Stat(lastProvidersPath()); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("the kept list is readable by others: %v", fi.Mode())
	}

	// uni-switch starts again: the list kept is served, marked as such
	forgetLastProviders()
	var last list
	if err := json.Unmarshal([]byte(get("/api/providers?last=1")), &last); err != nil {
		t.Fatal(err)
	}
	if !last.Last || !has(last, "own") {
		t.Fatalf("kept list after a start: %+v", last)
	}

	// kept by another version: not drawn
	b, _ := os.ReadFile(lastProvidersPath())
	os.WriteFile(lastProvidersPath(), []byte(strings.Replace(string(b), `"version":"`, `"version":"other-`, 1)), 0o600)
	forgetLastProviders()
	if b := get("/api/providers?last=1"); b != "null" {
		t.Fatalf("another version's list served: %.200s", b)
	}
}

// The tray panel's account switch (shuizhuyu on X) draws the kept list,
// so a change made through a provider or login action is kept at once,
// not only on the next GET /api/providers.
func TestKeptProvidersFollowActions(t *testing.T) {
	sandboxHome(t)
	forgetLastProviders()
	t.Cleanup(forgetLastProviders)
	if _, err := provider.Add(provider.Provider{ID: "own", Name: "Own", Chat: "http://127.0.0.1:1/v1", Key: "sk-own", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	srv := Handler(nil, nil)
	do := func(method, path, body string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	off := func() bool {
		t.Helper()
		var l struct {
			Last      bool `json:"last"`
			Providers []struct {
				ID  string `json:"id"`
				Off bool   `json:"off"`
			} `json:"providers"`
		}
		if err := json.Unmarshal([]byte(do("GET", "/api/providers?last=1", "")), &l); err != nil || !l.Last {
			t.Fatalf("kept list: %+v %v", l, err)
		}
		for _, p := range l.Providers {
			if p.ID == "own" {
				return p.Off
			}
		}
		t.Fatal("own is not in the kept list")
		return false
	}
	do("GET", "/api/providers", "")
	if off() {
		t.Fatal("own kept as off before it was switched off")
	}
	do("POST", "/api/provider/off", `{"id":"own"}`)
	if !off() {
		t.Error("the kept list still has own on after it was switched off")
	}
}
