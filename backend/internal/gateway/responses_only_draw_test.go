package gateway

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider added as "OpenAI Responses" only (what a Codex backend such as
// sub2api speaks) has no chat base, but it serves the images API under the
// same root as its Responses endpoint: its image models must be offered and
// drawn all the same.
func TestResponsesOnlyProviderDraws(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var paths, bodies []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths, bodies = append(paths, r.URL.Path), append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(pngBytes)+`"}],"usage":{"input_tokens":4,"output_tokens":9}}`)
	}))
	defer up.Close()

	// only the Responses base is set, as the add form's "OpenAI Responses" leaves it
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Responses: up.URL + "/v1", Key: "key"}); err != nil {
		t.Fatal(err)
	}
	// the relay's own /v1/models listed these: sub2api serves gpt-image-2.5
	if err := catalog.SaveLive("relay", up.URL+"/v1", []catalog.Model{
		{ID: "gpt-5.6-sol"},
		{ID: "gpt-image-2.5-flare", Draws: true},
	}); err != nil {
		t.Fatal(err)
	}

	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, m := range Drawers(*p) {
		got = append(got, m.ID)
	}
	if len(got) != 1 || got[0] != "gpt-image-2.5-flare" {
		t.Fatalf("Drawers = %v, want [gpt-image-2.5-flare]", got)
	}

	code, a, raw := postImages(t, New(), "/v1/images/generations", "application/json",
		`{"model":"relay/gpt-image-2.5-flare","prompt":"a uni-switch","size":"1024x1024"}`)
	if code != 200 || len(a.Data) != 1 {
		t.Fatalf("%d %s", code, raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/v1/images/generations" || !strings.Contains(bodies[0], `"model":"gpt-image-2.5-flare"`) {
		t.Fatalf("asked %v %v", paths, bodies)
	}
}
