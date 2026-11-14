package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestNewProviderQueueDefaults(t *testing.T) {
	sandboxHome(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	for _, tc := range []struct {
		id     string
		fields map[string]any
		limit  int
		wait   int
	}{
		{"defaults", nil, 20, 120},
		{"unlimited", map[string]any{"queueLimit": 0, "queueWait": 0}, 0, 0},
		{"nulls", map[string]any{"queueLimit": nil, "queueWait": nil}, 0, 0},
		{"custom", map[string]any{"queueLimit": 7, "queueWait": 15}, 7, 15},
		{"partial", map[string]any{"queueLimit": 5}, 5, 120},
	} {
		t.Run(tc.id, func(t *testing.T) {
			body := map[string]any{"new": true, "id": tc.id, "name": tc.id, "chat": upstream.URL + "/v1", "key": "qa-synthetic-" + tc.id, "models": []string{"qa-model"}}
			for k, v := range tc.fields {
				body[k] = v
			}
			post := func() {
				t.Helper()
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(string(data))))
				if w.Code != http.StatusOK {
					t.Fatalf("save: %d %s", w.Code, w.Body)
				}
			}
			check := func() {
				t.Helper()
				p, err := provider.Find(tc.id)
				if err != nil {
					t.Fatal(err)
				}
				if p.QueueLimit != tc.limit || p.QueueWait != tc.wait {
					t.Fatalf("queue defaults changed an explicit or saved setting: got %d/%d, want %d/%d", p.QueueLimit, p.QueueWait, tc.limit, tc.wait)
				}
			}
			post()
			check()
			body["new"] = false
			delete(body, "queueLimit")
			delete(body, "queueWait")
			post()
			check()
		})
	}
}
