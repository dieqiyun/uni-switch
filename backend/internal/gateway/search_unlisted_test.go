package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A relay on OpenAI's Responses API said to search the web by itself,
// whose models are typed in by hand because it has no model list, can be
// named as the searcher with those models: it was left out of the picker
// while Settings named it among the relays to pick (axelburks on Discord).
func TestSearcherRelayWithoutModelList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setHome(t, t.TempDir())
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	srv := httptest.NewServer(http.NotFoundHandler()) // no /models
	t.Cleanup(srv.Close)
	p := provider.Provider{ID: "relay", Name: "Relay", Key: "k", Searches: true, Responses: srv.URL + "/v1", Models: []string{"gpt-5.5", "gpt-5-mini"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err == nil {
		t.Fatal("the relay listed models")
	}
	cs := Searchers()
	if len(cs) != 1 || cs[0].Provider.ID != "relay" || !cs[0].ManualOnly {
		t.Fatalf("searchers = %+v", cs)
	}
	var ids []string
	for _, m := range cs[0].Models {
		ids = append(ids, m.ID)
	}
	if len(ids) != 2 || ids[0] != "gpt-5.5" || ids[1] != "gpt-5-mini" {
		t.Errorf("models = %v", ids)
	}
	if cs[0].Small == "" {
		t.Error("no model to search with when none is named")
	}
	st := settings.Load()
	st.Searcher = "relay/gpt-5.5"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if got, m, ok := searcher(); !ok || got.ID != "relay" || m != "gpt-5.5" {
		t.Errorf("searcher = %s %s %v", got.ID, m, ok)
	}
	if why := SearcherUnused(); why != "" {
		t.Errorf("unused: %q", why)
	}
}
