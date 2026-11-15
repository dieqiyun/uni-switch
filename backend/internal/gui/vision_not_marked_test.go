package gui

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// nexnull on Discord: a custom provider's model nothing says takes images
// isn't one Image recognition can be given, and Settings is told which
// providers have such a model so it can say where it is marked. Marked
// there, it is among the models; marked off, it isn't said again.
func TestVisionNotMarked(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	if err := provider.Save(provider.Provider{ID: "relay", Name: "My Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"house-model"}}); err != nil {
		t.Fatal(err)
	}
	srv := Handler(nil, nil)
	get := func() (unmarked, vision []string) {
		t.Helper()
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/settings", nil)
		r.RemoteAddr = "127.0.0.1:50000"
		srv.ServeHTTP(rec, r)
		var out struct {
			VisionNotMarked []string `json:"visionNotMarked"`
			VisionModels    []struct {
				ID string `json:"id"`
			} `json:"visionModels"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		for _, m := range out.VisionModels {
			vision = append(vision, m.ID)
		}
		return out.VisionNotMarked, vision
	}
	if u, v := get(); !slices.Equal(u, []string{"My Relay"}) || slices.Contains(v, "relay/house-model") {
		t.Fatalf("not marked: told %q, models %q", u, v)
	}
	for _, said := range []bool{true, false} {
		s := settings.Load()
		s.ModelImages = map[string]bool{"relay/house-model": said}
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
		if u, v := get(); len(u) != 0 || slices.Contains(v, "relay/house-model") != said {
			t.Fatalf("marked %v: told %q, models %q", said, u, v)
		}
	}
}
