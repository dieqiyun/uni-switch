package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func imageSetupRequest(mux *http.ServeMux, method, body string) *httptest.ResponseRecorder {
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, httptest.NewRequest(method, "/api/image-provider", strings.NewReader(body)))
	return rw
}

func TestImageProviderKeyOnlySetup(t *testing.T) {
	sandboxHome(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer qa-image-key", "Bearer qa-image-new-key":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-image-1","name":"Image One"},{"id":"qa-custom-drawer","kind":"image"},{"id":"qa-chat"},{"id":"sora-2"},{"id":"text-embedding-3-small"}]}`))
		case "Bearer qa-no-images":
			_, _ = w.Write([]byte(`{"data":[{"id":"qa-chat"}]}`))
		case "Bearer qa-empty":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key qa-bad-key"}}`))
		}
	}))
	defer upstream.Close()
	base := upstream.URL + "/v1"
	mux := http.NewServeMux()
	imageProviderRoutes(mux, base)
	if err := provider.Save(provider.Provider{ID: "ordinary", Name: "Chat provider", Chat: base, Key: "qa-chat-key", Models: []string{"qa-chat"}}); err != nil {
		t.Fatal(err)
	}
	if r := imageSetupRequest(mux, "GET", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"configured":false`) {
		t.Fatalf("initial state: %d %s", r.Code, r.Body)
	}
	if _, err := provider.Find(imageProviderID); err == nil {
		t.Fatal("reading setup created a provider")
	}
	r := imageSetupRequest(mux, "POST", `{"key":" qa-image-key ","chat":"https://ignored.invalid/v1"}`)
	if r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body)
	}
	if strings.Contains(r.Body.String(), "qa-image-key") {
		t.Fatal("response exposed the key")
	}
	p, err := provider.Find(imageProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != base || p.Key != "qa-image-key" || p.Responses != "" || p.Anthropic != "" || p.Gemini != "" || p.QueueLimit != 20 || p.QueueWait != 120 {
		t.Fatalf("wrong endpoint or defaults: %+v", p)
	}
	drawers := gateway.OfferedDrawers(*p)
	if len(drawers) != 2 {
		t.Fatalf("image models not available to generation: %+v", drawers)
	}
	if len(catalog.Chat(drawers)) != 0 {
		t.Fatal("image models were offered as chat models")
	}
	concurrency := 3
	p.QueueLimit, p.QueueWait, p.MaxConcurrency = 7, 15, &concurrency
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `{"key":"qa-image-new-key"}`} {
		if r := imageSetupRequest(mux, "POST", body); r.Code != 200 {
			t.Fatalf("refresh: %d %s", r.Code, r.Body)
		}
	}
	p, err = provider.Find(imageProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "qa-image-new-key" || p.QueueLimit != 7 || p.QueueWait != 15 || p.MaxConcurrency == nil || *p.MaxConcurrency != 3 {
		t.Fatal("refresh lost provider settings")
	}
	for _, key := range []string{"qa-bad-key", "qa-no-images", "qa-empty", ""} {
		body, _ := json.Marshal(map[string]string{"key": key})
		r := imageSetupRequest(mux, "POST", string(body))
		if r.Code == 200 {
			t.Fatalf("invalid list/key succeeded: %q", key)
		}
		if key != "" && strings.Contains(r.Body.String(), key) {
			t.Fatal("failure exposed the key")
		}
		p, _ = provider.Find(imageProviderID)
		if p.Key != "qa-image-new-key" || len(catalog.LiveDrawers(p.ID)) != 2 {
			t.Fatal("failed validation replaced the working setup")
		}
	}
	ordinary, err := provider.Find("ordinary")
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Key != "qa-chat-key" || len(ordinary.Models) != 1 {
		t.Fatal("image setup changed the ordinary provider")
	}
	r = imageSetupRequest(mux, "GET", "")
	if r.Code != 200 || strings.Contains(r.Body.String(), "qa-image-new-key") || !strings.Contains(r.Body.String(), `"configured":true`) {
		t.Fatalf("reopen: %d %s", r.Code, r.Body)
	}
}

func TestImageProviderOnlyAppearsInImageSetup(t *testing.T) {
	sandboxHome(t)
	p := provider.Provider{ID: imageProviderID, Name: "Saved image setup", Chat: imageProviderBase, Key: "qa-image-key"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive(p.ID, p.Chat, []catalog.Model{{ID: "gpt-image-1", Name: "Image One", Draws: true}}); err != nil {
		t.Fatal(err)
	}
	// A manually configured provider is still an ordinary provider, even at
	// the same endpoint. Only the dedicated setup row leaves the directory.
	if err := provider.Save(provider.Provider{ID: "manual-image-api", Name: "My API", Chat: imageProviderBase, Key: "qa-manual-key", Models: []string{"qa-chat"}}); err != nil {
		t.Fatal(err)
	}
	checkDirectory := func() {
		t.Helper()
		s := providersState()
		if len(s.Providers) != 1 || s.Providers[0].ID != "manual-image-api" {
			t.Fatalf("image setup leaked into ordinary providers: %+v", s.Providers)
		}
		for _, pr := range s.Presets {
			if pr.ID == "dieqiyun" && pr.Added {
				t.Fatal("image setup marked the chat preset as already added")
			}
		}
	}
	checkDirectory()
	for _, m := range provider.Catalog() {
		if m.Provider.ID == imageProviderID {
			t.Fatal("image setup leaked into the client's chat model catalog")
		}
	}
	s := settingsState()
	found := false
	for _, m := range s.ImageGenModels {
		if m.ID == imageProviderID+"/gpt-image-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("hiding the directory row removed its image model")
	}
	mux := http.NewServeMux()
	imageProviderRoutes(mux, imageProviderBase)
	if r := imageSetupRequest(mux, "GET", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"configured":true`) || !strings.Contains(r.Body.String(), "gpt-image-1") {
		t.Fatalf("saved setup is no longer editable: %d %s", r.Code, r.Body)
	}
	// Legacy Off rows do not move into the ordinary disabled-provider list.
	p.Off = true
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	checkDirectory()
	p.Off, p.Chat = false, "https://custom.invalid/v1"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, row := range providersState().Providers {
		if row.ID == imageProviderID {
			visible = true
		}
	}
	if !visible {
		t.Fatal("a conflicting custom configuration was hidden, preventing repair")
	}
}

func TestImageProviderPreservesConflictsAndUnreadableData(t *testing.T) {
	sandboxHome(t)
	mux := http.NewServeMux()
	imageProviderRoutes(mux, imageProviderBase)
	if err := provider.Save(provider.Provider{ID: imageProviderID, Chat: "https://custom.invalid/v1", Key: "qa-custom-key"}); err != nil {
		t.Fatal(err)
	}
	if r := imageSetupRequest(mux, "POST", `{"key":"qa-new-key"}`); r.Code == 200 || !strings.Contains(r.Body.String(), "image_provider_conflict") {
		t.Fatalf("conflict: %d %s", r.Code, r.Body)
	}
	if err := os.WriteFile(provider.Path(), []byte(`not valid JSON`), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(provider.Path())
	for _, method := range []string{"GET", "POST"} {
		if r := imageSetupRequest(mux, method, `{"key":"qa-new-key"}`); r.Code == 200 {
			t.Fatal("unreadable configuration treated as empty")
		}
	}
	after, _ := os.ReadFile(provider.Path())
	if string(before) != string(after) {
		t.Fatal("unreadable configuration overwritten")
	}
}

func TestImageProviderCanceledKeyCannotReplaceNewKey(t *testing.T) {
	sandboxHome(t)
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer qa-old-key" {
			close(started)
			<-release
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-image-1"}]}`))
	}))
	defer upstream.Close()
	defer close(release)
	mux := http.NewServeMux()
	imageProviderRoutes(mux, upstream.URL+"/v1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", "/api/image-provider", strings.NewReader(`{"key":"qa-old-key"}`)).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); mux.ServeHTTP(httptest.NewRecorder(), r) }()
	<-started
	cancel()
	if rw := imageSetupRequest(mux, "POST", `{"key":"qa-new-key"}`); rw.Code != 200 {
		t.Fatalf("new key: %d %s", rw.Code, rw.Body)
	}
	<-done
	p, err := provider.Find(imageProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "qa-new-key" {
		t.Fatal("canceled request saved an old key")
	}
}
