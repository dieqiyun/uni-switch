package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func endpointPost(t *testing.T, mux *http.ServeMux, action string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(string(data))))
	return w
}

func TestRequestEndpointSave(t *testing.T) {
	sandboxHome(t)
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	for _, endpoint := range []string{"auto", "chat", "responses", "anthropic"} {
		t.Run(endpoint, func(t *testing.T) {
			body := map[string]any{
				"new": true, "id": "endpoint-" + endpoint, "name": "QA " + endpoint, "preset": "dieqiyun",
				"chat": "https://qa.invalid/v1", "responses": "https://qa.invalid/v1", "anthropic": "https://qa.invalid",
				"endpointAPI": endpoint, "key": "qa-synthetic-" + endpoint, "models": []string{"qa-model"},
			}
			for _, isNew := range []bool{true, false} {
				body["new"] = isNew
				w := endpointPost(t, mux, "save", body)
				if w.Code != http.StatusOK {
					t.Fatalf("save: %d %s", w.Code, w.Body)
				}
				p, err := provider.Find(body["id"].(string))
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range map[string]string{"chat": p.Chat, "responses": p.Responses, "anthropic": p.Anthropic} {
					want := ""
					if endpoint == "auto" || endpoint == k {
						want = body[k].(string)
					}
					if v != want {
						t.Fatalf("%s URL = %q, want %q", k, v, want)
					}
				}
			}
		})
	}
	w := endpointPost(t, mux, "save", map[string]any{"new": true, "id": "invalid-endpoint", "endpointAPI": "typo"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown endpoint accepted: %d", w.Code)
	}
}

func TestRequestEndpointTypedTest(t *testing.T) {
	sandboxHome(t)
	var mu sync.Mutex
	var probes []string
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"qa-model"}]}`))
			return
		}
		mu.Lock()
		probes = append(probes, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer qa-typed-key" && r.Header.Get("X-Api-Key") != "qa-typed-key" {
			t.Error("test did not use the draft key")
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"synthetic permission denied"}}`))
	}))
	defer vendor.Close()
	p := provider.Provider{ID: "qa-endpoints", Name: "QA", Key: "qa-saved-key", Proxy: "direct",
		Chat: vendor.URL + "/v1", Responses: vendor.URL + "/v1", Anthropic: vendor.URL, Models: []string{"qa-model"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	for _, tc := range []struct{ endpoint, path string }{
		{"chat", "/v1/chat/completions"}, {"responses", "/v1/responses"}, {"anthropic", "/v1/messages"},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			mu.Lock()
			probes = nil
			mu.Unlock()
			w := endpointPost(t, mux, "test", map[string]any{"id": p.ID, "typed": true,
				"key": "qa-typed-key", "endpointAPI": tc.endpoint, tc.endpoint: map[string]string{
					"chat": p.Chat, "responses": p.Responses, "anthropic": p.Anthropic,
				}[tc.endpoint]})
			if w.Code != http.StatusOK {
				t.Fatalf("test: %d %s", w.Code, w.Body)
			}
			var got struct{ Results []provider.Result }
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Results) != 1 || string(got.Results[0].Protocol) != tc.endpoint || got.Results[0].Status != http.StatusForbidden {
				t.Fatalf("test restored another endpoint: %s", w.Body)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(probes) == 0 {
				t.Fatal("no request sent")
			}
			for _, path := range probes {
				if path != tc.path {
					t.Errorf("unselected endpoint requested: %s", path)
				}
			}
		})
	}
	saved, err := provider.Find(p.ID)
	if err != nil || saved.Key != p.Key || !reflect.DeepEqual(saved.Speaks(), p.Speaks()) {
		t.Fatalf("test persisted draft credentials or endpoint: %v", err)
	}
}
