package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// zoecats (Discord, 10-25): the editor's "Set it aside when out of quota or
// rate limited" tick is saved on a provider of the user's, opens as
// saved, and is kept by a Save that leaves it out.
func TestProviderSaveLimitRestOff(t *testing.T) {
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
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	off := func() bool {
		t.Helper()
		p, err := provider.Find("agg")
		if err != nil {
			t.Fatal(err)
		}
		if info := providerInfo(*p, nil); info.LimitRestOff != p.LimitRestOff {
			t.Fatalf("the editor is shown %v, saved %v", info.LimitRestOff, p.LimitRestOff)
		}
		return p.LimitRestOff
	}
	const edit = `{"id":"agg","from":"agg","name":"Aggregator","chat":"http://127.0.0.1:8317/v1","icon":"generic","models":[],"headers":{}`

	post(`{"id":"agg","name":"Aggregator","key":"sk-a","chat":"http://127.0.0.1:8317/v1","icon":"generic","new":true}`)
	if off() {
		t.Fatal("added without it, saved never resting")
	}
	post(edit + `,"limitRestOff":true}`)
	if !off() {
		t.Fatal("unticked, still rests")
	}
	post(edit + `}`)
	if !off() {
		t.Fatal("a Save that left it out turned rests back on")
	}
	post(edit + `,"limitRestOff":false}`)
	if off() {
		t.Fatal("ticked again, still never rests")
	}
	post(`{"id":"agg2","name":"Aggregator 2","key":"sk-b","chat":"http://127.0.0.1:8318/v1","icon":"generic","new":true,"limitRestOff":true}`)
	if p, _ := provider.Find("agg2"); p == nil || !p.LimitRestOff {
		t.Fatal("added unticked, saved resting")
	}
}
