package gui

// Best in family (Steven on Discord): the editor's fourth option is saved
// as bestInFamily with no model picked, kept by a save that doesn't name
// it, and the card serves the newest of each family; each model says
// whether it is one (best), so the editor draws them before a save.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestProviderSaveBestInFamily(t *testing.T) {
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
	type model struct {
		ID   string `json:"id"`
		On   bool   `json:"on"`
		Best bool   `json:"best"`
	}
	card := func() (best bool, on, newest []string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
		var got struct {
			Providers []struct {
				ID           string  `json:"id"`
				BestInFamily bool    `json:"bestInFamily"`
				Models       []model `json:"models"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		for _, p := range got.Providers {
			if p.ID != "relay" {
				continue
			}
			for _, m := range p.Models {
				if m.On {
					on = append(on, m.ID)
				}
				if m.Best {
					newest = append(newest, m.ID)
				}
			}
			return p.BestInFamily, on, newest
		}
		t.Fatal("relay not listed")
		return
	}

	const chat = "http://127.0.0.1:1/v1"
	post(`{"id":"relay","name":"Relay","key":"sk-r","chat":"` + chat + `","new":true}`)
	ms := []catalog.Model{{ID: "gemini-3.5-flash"}, {ID: "gemini-2.5-flash"}, {ID: "gemini-3.1-pro-preview"}, {ID: "gemini-2.5-pro"}, {ID: "gemini-3.5-flash-lite"}}
	if err := catalog.SaveLive("relay", chat, ms); err != nil {
		t.Fatal(err)
	}
	newestWant := []string{"gemini-3.5-flash", "gemini-3.1-pro-preview", "gemini-3.5-flash-lite"}
	if best, on, newest := card(); best || len(on) != 5 || !slices.Equal(newest, newestWant) {
		t.Fatalf("before: best %v on %v newest %v", best, on, newest)
	}

	post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","models":[],"noModels":false,"bestInFamily":true,"headers":{}}`)
	if best, on, _ := card(); !best || !slices.Equal(on, newestWant) {
		t.Fatalf("best: %v on %v", best, on)
	}

	// a save that doesn't name it (another editor's, the CLI's) keeps it
	post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","headers":{}}`)
	if best, on, _ := card(); !best || !slices.Equal(on, newestWant) {
		t.Errorf("not named: best %v on %v", best, on)
	}

	// Vendor's list: the editor sends it false
	post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","models":[],"noModels":false,"bestInFamily":false,"headers":{}}`)
	if best, on, _ := card(); best || len(on) != 5 {
		t.Errorf("vendor's list: best %v on %v", best, on)
	}

	// a pick clears it, even sent with it
	post(`{"id":"relay","from":"relay","name":"Relay","key":"","chat":"` + chat + `","models":["gemini-2.5-pro"],"noModels":false,"bestInFamily":true,"headers":{}}`)
	if p, _ := provider.Find("relay"); p.BestInFamily {
		t.Error("a pick kept Best in family")
	}
	if best, on, _ := card(); best || !slices.Equal(on, []string{"gemini-2.5-pro"}) {
		t.Errorf("picked: best %v on %v", best, on)
	}
}
