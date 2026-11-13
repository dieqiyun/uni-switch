package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The editor's Compaction row (#1099): a Save that sends nativeCompaction
// sets it, one that leaves it out keeps it, and a provider without a
// Responses API is refused it. /api/providers says which providers can
// carry it, for the editor to offer the row.
func TestProviderSaveNativeCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	on := func(id string) bool {
		t.Helper()
		p, err := provider.Find(id)
		if err != nil {
			t.Fatal(err)
		}
		return p.NativeCompaction
	}
	if w := post(`{"id":"relay","name":"Relay","key":"sk-r","responses":"http://127.0.0.1:1/v1","icon":"generic","new":true,"nativeCompaction":true}`); w.Code != 200 || !on("relay") {
		t.Fatalf("new, on: %d %s", w.Code, w.Body)
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","key":"","responses":"http://127.0.0.1:1/v1","icon":"generic","models":[],"headers":{}}`); w.Code != 200 || !on("relay") {
		t.Fatalf("a save without it: %d %s", w.Code, w.Body)
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","key":"","responses":"http://127.0.0.1:1/v1","icon":"generic","models":[],"headers":{},"nativeCompaction":false}`); w.Code != 200 || on("relay") {
		t.Fatalf("off: %d %s", w.Code, w.Body)
	}
	if w := post(`{"id":"chat","name":"Chat","key":"sk-c","chat":"http://127.0.0.1:1/v1","icon":"generic","new":true,"nativeCompaction":true}`); w.Code == 200 || !strings.Contains(w.Body.String(), "can't carry") {
		t.Fatalf("a chat-only provider took it: %d %s", w.Code, w.Body)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	var got struct {
		Providers []struct {
			ID                string `json:"id"`
			CarriesCompaction bool   `json:"carriesCompaction"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v %s", err, w.Body)
	}
	carries := map[string]bool{}
	for _, p := range got.Providers {
		carries[p.ID] = p.CarriesCompaction
	}
	if !carries["relay"] {
		t.Errorf("the Responses relay isn't said to carry it: %s", w.Body)
	}
}
