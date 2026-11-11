package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// uni-switch provider refresh-every and auto-include set a provider's list
// schedule and rules (#2135); auto-include takes the listed models its
// rules match at once, none clears the rules, and a time or a rule uni-switch
// can't read is refused and changes nothing.
func TestRefreshEveryAndAutoIncludeCmds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DSH_HOME", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "gpt-6"}, {"id": "glm-5"}, {"id": "gpt-6-mini"}}})
	}))
	defer srv.Close()
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k1", Chat: srv.URL + "/v1", Models: []string{"glm-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := providerCmd([]string{"provider", "refresh", "relay"}); err != nil {
		t.Fatal(err)
	}
	find := func() *provider.Provider {
		p, err := provider.Find("relay")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := providerCmd([]string{"provider", "refresh-every", "relay", "12h"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"1m", "soon"} {
		if err := providerCmd([]string{"provider", "refresh-every", "relay", bad}); err == nil {
			t.Fatalf("%s taken", bad)
		}
	}
	if p := find(); p.RefreshEvery != "12h" {
		t.Fatalf("every %q, want 12h", p.RefreshEvery)
	}
	if err := providerCmd([]string{"provider", "auto-include", "relay", "gpt-6"}); err == nil {
		t.Fatal("a model's id taken as a rule")
	}
	if err := providerCmd([]string{"provider", "auto-include", "relay", "gpt-*"}); err != nil {
		t.Fatal(err)
	}
	if p := find(); !slices.Equal(p.Models, []string{"gpt-6", "gpt-6-mini", "glm-5"}) || !slices.Equal(p.AutoInclude, []string{"gpt-*"}) {
		t.Fatalf("picks %v, rules %v", p.Models, p.AutoInclude)
	}
	if err := providerCmd([]string{"provider", "auto-include", "relay", "none"}); err != nil {
		t.Fatal(err)
	}
	if err := providerCmd([]string{"provider", "refresh-every", "relay", "off"}); err != nil {
		t.Fatal(err)
	}
	if p := find(); len(p.AutoInclude) != 0 || p.RefreshEvery != "" || !slices.Equal(p.Models, []string{"gpt-6", "gpt-6-mini", "glm-5"}) {
		t.Fatalf("after none and off: rules %v, every %q, picks %v", p.AutoInclude, p.RefreshEvery, p.Models)
	}
}
