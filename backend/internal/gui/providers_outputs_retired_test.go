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

// The reporter's case through the editor's Save (#3305, LoKi): a Max
// output set on a model the provider has since stopped listing
// (opencode-zen-free/space-bunny-free=65536) is given to the editor and
// sent back with every Save, with no model picked too. The Save goes
// through and keeps it; a newly typed unknown model is still refused.
func TestProviderSaveKeepsRetiredModelOutput(t *testing.T) {
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
	if w := post(`{"id":"relay","name":"Relay","key":"k","chat":"http://127.0.0.1:1/v1","models":["sol","space-bunny-free"],"new":true}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	const head = `{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1"`
	if w := post(head + `,"models":["sol","space-bunny-free"],"outputs":{"space-bunny-free":65536},"compacts":{"space-bunny-free":200000}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// the model leaves the list
	if w := post(head + `,"models":["sol"],"outputs":{"space-bunny-free":65536},"compacts":{"space-bunny-free":200000}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if provider.ServesModel(*p, "space-bunny-free") {
		t.Fatal("space-bunny-free is still served; the test needs it gone")
	}
	info := providerInfo(*p, nil)
	if info.Outputs["space-bunny-free"] != 65536 || info.Compacts["space-bunny-free"] != 200000 {
		t.Fatalf("the editor is given %v and %v", info.Outputs, info.Compacts)
	}
	// Select none, the rest as the editor has it
	if w := post(head + `,"models":[],"noModels":true,"outputs":{"space-bunny-free":65536},"compacts":{"space-bunny-free":200000}}`); w.Code != 200 {
		t.Fatalf("a Save with no model picked: %d %s", w.Code, w.Body)
	}
	if w := post(head + `,"models":["sol"],"outputs":{"space-bunny-free":65536,"sol":32000},"compacts":{"space-bunny-free":200000}}`); w.Code != 200 {
		t.Fatalf("a Save with the stale limit beside a new one: %d %s", w.Code, w.Body)
	}
	want := map[string]int{"relay/space-bunny-free": 65536, "relay/sol": 32000}
	if got := settings.Load().ModelOutputs; !maps.Equal(got, want) {
		t.Errorf("outputs %v; want %v", got, want)
	}
	if got := settings.Load().ModelCompacts; !maps.Equal(got, map[string]int{"relay/space-bunny-free": 200000}) {
		t.Errorf("compacts %v", got)
	}
	if w := post(head + `,"models":["sol"],"outputs":{"space-bunny-free":65536,"typo":5}}`); w.Code == 200 || !strings.Contains(w.Body.String(), "typo") {
		t.Fatalf("a newly typed model it doesn't serve: %d %s", w.Code, w.Body)
	}
	if w := post(head + `,"models":["sol"],"outputs":{"sol":32000},"compacts":{}}`); w.Code != 200 {
		t.Fatalf("cleared: %d %s", w.Code, w.Body)
	}
	if got := settings.Load().ModelOutputs; !maps.Equal(got, map[string]int{"relay/sol": 32000}) || len(settings.Load().ModelCompacts) != 0 {
		t.Errorf("cleared: %v %v", got, settings.Load().ModelCompacts)
	}
}
