package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

func TestListKeepsImageModelsSeparate(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-sol"},{"id":"gpt-image-2.5-flare"},{"id":"sora-2","kind":"video"}]}`))
	}))
	defer server.Close()

	p := Provider{ID: "relay", Name: "Relay", Responses: server.URL + "/v1", Key: "k"}
	ms, err := p.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"gpt-5.6-sol"}) {
		t.Fatalf("List = %v, want only chat models", ids)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	images := catalog.LiveDrawers(p.ID)
	if len(images) != 1 || images[0].ID != "gpt-image-2.5-flare" {
		t.Fatalf("LiveDrawers = %v", images)
	}
	if videos := catalog.LiveVideomakers(p.ID); len(videos) != 1 || videos[0].ID != "sora-2" {
		t.Fatalf("LiveVideomakers = %v", videos)
	}
}
