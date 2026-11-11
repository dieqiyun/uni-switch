package gui

// #1818: the editor's Select none is saved as noModels with no model
// picked, kept by a save that doesn't name it, and the card then counts
// none of the provider's models; a pick clears it.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestProviderSaveSelectNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	card := func() (exposed int, none bool) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
		var got struct {
			Providers []struct {
				ID       string `json:"id"`
				Exposed  int    `json:"exposed"`
				NoModels bool   `json:"noModels"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		for _, p := range got.Providers {
			if p.ID == "relay" {
				return p.Exposed, p.NoModels
			}
		}
		t.Fatal("relay not listed")
		return
	}

	const chat = "http://127.0.0.1:1/v1"
	post(`{"id":"relay","name":"Relay","key":"sk-r","chat":"` + chat + `","new":true}`)
	var ms []catalog.Model
	for i := range 30 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("m%d", i)})
	}
	if err := catalog.SaveLive("relay", chat, ms); err != nil {
		t.Fatal(err)
	}
	if n, none := card(); n != 24 || none {
		t.Fatalf("before: exposed %d none %v", n, none)
	}

	// the reporter's case: Select none, with and without Only through
	// routing groups
	for _, unlisted := range []string{"false", "true"} {
		post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","models":[],"noModels":true,"unlisted":` + unlisted + `,"headers":{}}`)
		p, _ := provider.Find("relay")
		if !p.NoModels || len(p.Exposed()) != 0 {
			t.Errorf("unlisted=%s: noModels %v exposed %d", unlisted, p.NoModels, len(p.Exposed()))
		}
		if n, none := card(); n != 0 || !none {
			t.Errorf("unlisted=%s card: exposed %d none %v", unlisted, n, none)
		}
	}

	// a save that doesn't name it (another editor's, the CLI's) keeps it
	post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","headers":{}}`)
	if n, none := card(); n != 0 || !none {
		t.Errorf("not named: exposed %d none %v", n, none)
	}

	// a pick clears it
	post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","models":["m3"],"noModels":false,"headers":{}}`)
	if n, none := card(); n != 1 || none {
		t.Errorf("picked: exposed %d none %v", n, none)
	}
}
