package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The name the user gave a provider's model, and the reasoning levels they
// kept of it, are what /v1/models lists; the same model from another
// provider keeps its own, and the id stays the same.
func TestModelsListUsersNameAndLevels(t *testing.T) {
	fresh(t)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	data := `{"a":{"models":{"sol":{"id":"sol","name":"Sol","reasoning_options":[{"type":"effort","values":["low","medium","high","max"]}]}}},
		"b":{"models":{"sol":{"id":"sol","name":"Sol","reasoning_options":[{"type":"effort","values":["low","medium","high","max"]}]}}}}`
	if err := os.WriteFile(catalog.CachePath(), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for _, id := range []string{"a", "b"} {
		if err := provider.Save(provider.Provider{ID: id, Name: strings.ToUpper(id), Catalog: id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"sol"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.SetModelName("a/sol", "Sol 5"); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelEfforts("a/sol", []string{"low", "high"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	var out struct {
		Data []struct {
			ID     string `json:"id"`
			Name   string `json:"display_name"`
			Levels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range out.Data {
		var ls []string
		for _, l := range m.Levels {
			ls = append(ls, l.Effort)
		}
		got[m.ID] = m.Name + " " + strings.Join(ls, ",")
	}
	if got["a/sol"] != "Sol 5 low,high" || got["b/sol"] != "Sol low,medium,high,max" {
		t.Fatalf("%v\n%s", got, rec.Body.String())
	}
}
