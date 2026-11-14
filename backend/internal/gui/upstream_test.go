package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/upstream"
)

// /api/upstream says what the vendor's page says for the providers that
// call its API, and nothing for a relay serving the same models (#971)
func TestUpstreamRoute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"components":[{"id":"api","name":"Claude API (api.anthropic.com)","status":"major_outage","group":false},
{"id":"web","name":"claude.ai","status":"operational","group":false}],
"incidents":[{"name":"Claude API errors","status":"investigating","impact":"critical","shortlink":"https://stspg.io/z","components":[{"id":"api"}]}]}`))
	}))
	defer page.Close()
	was := slices.Clone(upstream.Vendors)
	defer func() { upstream.Vendors = was; upstream.Forget() }()
	for i := range upstream.Vendors {
		upstream.Vendors[i].Summary = "" // no other page is asked
		if upstream.Vendors[i].ID == "anthropic" {
			upstream.Vendors[i].Summary = page.URL
		}
	}
	upstream.Forget()
	for _, p := range []provider.Provider{
		{ID: "anth", Name: "Anthropic", Anthropic: "https://api.anthropic.com", Key: "k"},
		{ID: "relay", Name: "Relay", Anthropic: "https://relay.example.invalid", Key: "k"},
		{ID: "ds", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/upstream", nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		Vendors   []upstream.Status
		Providers map[string]string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Providers["anth"] != "anthropic" || got.Providers["ds"] != "deepseek" || got.Providers["relay"] != "" {
		t.Fatalf("providers %v", got.Providers)
	}
	i := slices.IndexFunc(got.Vendors, func(s upstream.Status) bool { return s.Vendor == "anthropic" })
	if i < 0 || got.Vendors[i].Level != "major" || len(got.Vendors[i].Incidents) != 1 || got.Vendors[i].Incidents[0].URL != "https://stspg.io/z" {
		t.Fatalf("vendors %+v", got.Vendors)
	}
	j := slices.IndexFunc(got.Vendors, func(s upstream.Status) bool { return s.Vendor == "deepseek" })
	if j < 0 || got.Vendors[j].Level != "" || got.Vendors[j].Page != "https://status.deepseek.com" {
		t.Fatalf("deepseek %+v", got.Vendors)
	}
}
