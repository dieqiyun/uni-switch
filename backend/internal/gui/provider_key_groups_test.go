package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestAPIKeyGroupsSaveCustomAndPreset(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"shared"}]}`))
	}))
	defer upstream.Close()
	pr := provider.Preset("dieqiyun")
	if pr == nil {
		t.Fatal("missing relay preset")
	}
	old := *pr
	defer func() { *pr = old }()
	pr.Chat, pr.Responses, pr.Anthropic = upstream.URL+"/v1", "", ""
	for _, preset := range []string{"", "dieqiyun"} {
		t.Run("preset="+preset, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("USERPROFILE", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			mux := http.NewServeMux()
			providerRoutes(mux, nil)
			body := map[string]any{"new": true, "id": "key-groups", "name": "Relay", "key": "qa-basic", "keyName": "普通组", "keyGroups": true, "models": []string{"shared"}}
			if preset == "" {
				body["chat"] = upstream.URL + "/v1"
			} else {
				body["preset"] = preset
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
				p, err := provider.Find("key-groups")
				if err != nil {
					t.Fatal(err)
				}
				if !p.KeyGroups || p.KeyName != "普通组" {
					t.Fatalf("saved provider lost named key group: enabled=%v name=%q", p.KeyGroups, p.KeyName)
				}
			}
			post()
			check()
			// An ordinary editor save that omits group fields preserves them.
			body["new"], body["key"] = false, ""
			delete(body, "keyName")
			delete(body, "keyGroups")
			post()
			check()
		})
	}
}
