package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// info's count of the models is kept, and counted again once a provider
// is added. Another uni-switch asks every 15s or so, after the catalog a
// request held (provider.Hold) has gone: info is asked here outside one.
func TestInfoCountsModelsOnceAChange(t *testing.T) {
	var s *Server
	count := func() float64 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "127.0.0.1:5555"
		w := httptest.NewRecorder()
		s.info(w, r)
		var out struct{ Models float64 }
		if err := decodeJSON(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Models
	}
	// asked again with nothing changed, it costs the same whatever the
	// lists' length
	allocs := func(n int) float64 {
		manyEyes(t, n, true)
		s = New()
		if m := count(); m != float64(len(provider.Catalog())) {
			t.Fatalf("info says %v models of %d", m, len(provider.Catalog()))
		}
		return testing.AllocsPerRun(20, func() { count() })
	}
	few, many := allocs(4), allocs(400)
	t.Logf("allocations a GET /: %.0f with 4 models listed, %.0f with 400", few, many)
	if many > few+50 {
		t.Fatalf("GET / made %.0f allocations with 400 models listed, %.0f with 4: the catalog is built again", many, few)
	}
	n := count()
	if err := provider.Save(provider.Provider{ID: "more", Name: "More", Chat: "http://more.invalid/v1", Key: "k", Models: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if m := count(); m != float64(len(provider.Catalog())) || m == n {
		t.Fatalf("info says %v models after a provider was added, of %d", m, len(provider.Catalog()))
	}
}
