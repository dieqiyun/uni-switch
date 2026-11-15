package gui

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A provider's Save carries its editor's Max output, as it does its
// context window (ARNO on Discord: a model's maxTokens was wrong and only
// the window could be set there); a save that doesn't say keeps them, and
// the editor is given them back to show.
func TestProviderSaveOutputs(t *testing.T) {
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
	if w := post(`{"id":"relay","name":"Relay","key":"k","chat":"http://127.0.0.1:1/v1","models":["sol"],"new":true}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	const save = `{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"]`
	if w := post(save + `,"outputs":{"*":32000,"sol":128000}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := map[string]int{"relay/*": 32000, "relay/sol": 128000}
	if got := settings.Load().ModelOutputs; !maps.Equal(got, want) {
		t.Fatalf("outputs %v", got)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if got := providerInfo(*p, nil).Outputs; !maps.Equal(got, map[string]int{"*": 32000, "sol": 128000}) {
		t.Errorf("the editor is given %v", got)
	}
	if w := post(save + `}`); w.Code != 200 || !maps.Equal(settings.Load().ModelOutputs, want) {
		t.Fatalf("a save that doesn't say: %d %v", w.Code, settings.Load().ModelOutputs)
	}
	if w := post(save + `,"outputs":{"typo":5}}`); w.Code == 200 || !strings.Contains(w.Body.String(), "typo") {
		t.Fatalf("a model it doesn't serve: %d %s", w.Code, w.Body)
	}
	if w := post(save + `,"outputs":{}}`); w.Code != 200 || len(settings.Load().ModelOutputs) != 0 {
		t.Fatalf("emptied: %d %v", w.Code, settings.Load().ModelOutputs)
	}
}
