package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestUseProviderAPIConnectsAndReturnsPersistedSelection(t *testing.T) {
	home := t.TempDir()
	for key, value := range map[string]string{"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "CODEX_HOME": filepath.Join(home, ".codex")} {
		t.Setenv(key, value)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "synthetic", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
			t.Fatal(err)
		}
	}
	h := Handler(nil, nil)
	for _, id := range []string{"a", "b"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/agents/provider-models/codex?provider="+id, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), id+"/m1") {
			t.Fatal(rec.Code, rec.Body)
		}
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/agents/use-provider/codex", strings.NewReader(`{"provider":"`+id+`","model":"`+id+`/m1"}`)))
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body)
		}
		var s stateJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if s.Settings.AgentProviders["codex"] != id {
			t.Fatalf("selection missing: %s", rec.Body)
		}
		for _, a := range s.Agents {
			if a.ID == "codex" && (!a.Wired || a.Fields[0].Value != id+"/m1" || a.Models.Shown != 1) {
				t.Fatalf("client: %+v", a)
			}
		}
	}
}
