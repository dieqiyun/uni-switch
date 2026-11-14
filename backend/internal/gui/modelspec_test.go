package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What uni-switch model spec sets (Dani Ariyanto on Discord) is what the
// providers' state behind the app's model badges says: the window, the
// reply limit, images and the levels given a model nothing knew; a model
// sent no reasoning field is said to be so, with no level kept.
func TestModelSpecInProvidersState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "kenari", Name: "Kenari", Chat: "https://example.invalid/v1", Key: "k", Models: []string{"glm-5:free", "qwen-x:free"}}); err != nil {
		t.Fatal(err)
	}
	n := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	if err := provider.SetModelSpec("kenari/glm-5:free", provider.SpecChange{Context: n(131072), Output: n(16384), Vision: s("on"), Reasoning: s("low-high")}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelSpec("kenari/qwen-x:free", provider.SpecChange{Reasoning: s("off")}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	var state providersJSON
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	models := map[string]modelJSON{}
	for _, p := range state.Providers {
		if p.ID == "kenari" {
			for _, m := range p.Models {
				models[m.ID] = m
			}
		}
	}
	m := models["glm-5:free"]
	if m.Context != 131072 || m.Output != 16384 || !m.Images || !m.ImageSet || !m.Given || !slices.Equal(m.Kept, []string{"low", "medium", "high"}) {
		t.Fatalf("glm-5:free: %+v", m)
	}
	if q := models["qwen-x:free"]; !q.Reasonless || len(q.Kept) != 0 || q.Images || q.Context != 0 {
		t.Fatalf("qwen-x:free: %+v", q)
	}
}
