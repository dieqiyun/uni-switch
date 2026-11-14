package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider's list schedule and auto-include rules (#2135) are set from
// the CLI; the editor's Save, which doesn't send them, keeps them.
func TestProviderSaveKeepsListScheduleAndRules(t *testing.T) {
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
			t.Fatalf("save: %d %s", w.Code, w.Body)
		}
	}
	const u = "http://127.0.0.1:1/v1"
	post(`{"id":"relay","name":"Relay","key":"sk-a","chat":"` + u + `","icon":"generic","new":true}`)
	if err := provider.SetRefreshEvery("relay", "6h"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.SetAutoInclude("relay", []string{"gpt-*"}); err != nil {
		t.Fatal(err)
	}
	post(`{"id":"relay","from":"relay","name":"Relay 2","chat":"` + u + `","icon":"generic"}`)
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Relay 2" || p.RefreshEvery != "6h" || !slices.Equal(p.AutoInclude, []string{"gpt-*"}) {
		t.Fatalf("after the editor's save: name %q, every %q, rules %v", p.Name, p.RefreshEvery, p.AutoInclude)
	}
}
