package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// The same for a provider of the user's own whose list is fetched again,
// by Fetch models or by itself (RefreshEvery): a relay that leaves a model
// out of one list gives the pick back with the list that has it.
func TestRelayPicksComeBackWithTheirList(t *testing.T) {
	oneHome(t)
	var short atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		ids := []string{"m-a", "m-b", "m-c"}
		if short.Load() {
			ids = []string{"m-a", "m-c"}
		}
		var data []map[string]string
		for _, id := range ids {
			data = append(data, map[string]string{"id": id, "object": "model"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	t.Cleanup(srv.Close)
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: srv.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	refetch := func() []string {
		t.Helper()
		p, err := Find("relay")
		if err != nil {
			t.Fatal(err)
		}
		_, dropped, err := p.Refetch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return dropped
	}
	refetch()
	p, _ := Find("relay")
	p.Models = []string{"m-a", "m-b"}
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}
	short.Store(true)
	if got := refetch(); !slices.Equal(got, []string{"m-b"}) {
		t.Fatalf("dropped %v", got)
	}
	if got := servedIDs(t, "relay"); !slices.Equal(got, []string{"m-a"}) {
		t.Fatalf("served while m-b isn't listed: %v", got)
	}
	short.Store(false)
	refetch()
	if p, _ = Find("relay"); !slices.Equal(p.Models, []string{"m-a", "m-b"}) || len(p.Delisted) != 0 {
		t.Fatalf("listed again: picks %v, set aside %v", p.Models, p.Delisted)
	}
}
