package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A heap profile of a gateway at work (~1,000 requests in 10 minutes)
// had settings.json parsed many times for each request, ~800 MB in all:
// redaction, the model wires, efforts, images, conversations and member
// names each read the settings again. An unchanged file is parsed once
// however many requests are served, and what the requests share of it is
// left as the file says.
//
// Counted, not timed: before, each request parsed it seven times.
func TestRequestsParseUnchangedSettingsOnce(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	// a long-used uni-switch's settings: answers about many models
	images := map[string]bool{}
	for i := range 200 {
		images[fmt.Sprintf("p%d/model-%d", i%30, i)] = i%2 == 0
	}
	raw, err := json.Marshal(map[string]any{"modelImages": images, "fetchPrivate": []string{"hunter2"}, "modelWires": map[string]string{"relay/m1": "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	was, _ := os.ReadFile(settings.Path())
	t.Cleanup(func() { os.WriteFile(settings.Path(), was, 0o600) })
	os.MkdirAll(filepath.Dir(settings.Path()), 0o755)
	if err := os.WriteFile(settings.Path(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	t.Cleanup(gw.Close)
	ask := func() {
		t.Helper()
		res, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"relay/m1","messages":[{"role":"user","content":"my password is hunter2"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("status %d: %s", res.StatusCode, b)
		}
	}
	ask()
	const requests = 30
	before := settings.Parses()
	for range requests {
		ask()
	}
	if n := settings.Parses() - before; n >= requests {
		t.Fatalf("settings.json parsed %d times for %d requests with the file unchanged", n, requests)
	}
	var want settings.Settings
	json.Unmarshal(raw, &want)
	if got := settings.Shared(); !reflect.DeepEqual(got.ModelImages, want.ModelImages) || !reflect.DeepEqual(got.FetchPrivate, want.FetchPrivate) || !reflect.DeepEqual(got.ModelWires, want.ModelWires) {
		t.Fatalf("the requests changed the shared settings: %v %v", got.FetchPrivate, got.ModelWires)
	}
}
