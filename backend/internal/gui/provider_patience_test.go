package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider's Patience, set with `uni-switch provider set <id> patience=`
// (zoecats on Discord), is not in the editor: the editor's save keeps it.
func TestProviderSaveKeepsPatience(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, set := range []int{provider.PatienceOff, 150} {
		if err := provider.Save(provider.Provider{ID: "or", Name: "OpenRouter", Chat: "https://example.invalid/v1", Key: "k", Patience: set}); err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		providerRoutes(mux, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(`{"id":"or","name":"OpenRouter","chat":"https://example.invalid/v1"}`)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		got, err := provider.Find("or")
		if err != nil {
			t.Fatal(err)
		}
		if got.Patience != set {
			t.Fatalf("patience %d after the editor's save, want %d kept", got.Patience, set)
		}
	}
}
