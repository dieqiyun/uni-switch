package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// keys/import adds the keys pasted at once (361 on Discord), says how many
// were new, and a key the gateway rests says so in its row.
func TestKeysImportAndRest(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	p := provider.Provider{ID: "import-test", Name: "Import", Chat: "https://example.invalid/v1", Key: "first"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(10 * time.Minute)
	was := keyRestOf
	keyRestOf = func(key string) (gateway.Rest, bool) {
		if key == p.ID+"#"+provider.KeyID("two") {
			return gateway.Rest{Why: "rate", Status: 429, Until: until}, true
		}
		return gateway.Rest{}, false
	}
	defer func() { keyRestOf = was }()
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/keys/import", strings.NewReader(`{"id":"import-test","key":"first\ntwo, three\n\n'four'"}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var st providersJSON
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Added != 3 || st.Had != 1 {
		t.Fatalf("added %d had %d", st.Added, st.Had)
	}
	var info *providerJSON
	for i := range st.Providers {
		if st.Providers[i].ID == p.ID {
			info = &st.Providers[i]
		}
	}
	if info == nil || len(info.KeyList) != 4 {
		t.Fatalf("keys: %+v", info)
	}
	for i, k := range info.KeyList {
		if (k.Rest != nil) != (i == 1) {
			t.Fatalf("key %d rest %+v", i, k.Rest)
		}
	}
	if r := info.KeyList[1].Rest; r.Status != 429 || r.Why != "rate" || !r.Until.Equal(until) {
		t.Fatalf("rest %+v", r)
	}
	for _, k := range []string{`"first"`, `"two"`, `"three"`, `"four"`} {
		if strings.Contains(w.Body.String(), `"key":`+k) {
			t.Fatal("leaked a key")
		}
	}
	// pasting only keys it has is refused, saying so
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/keys/import", strings.NewReader(`{"id":"import-test","key":"two three"}`)))
	if w.Code == 200 || !strings.Contains(w.Body.String(), "already has") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
