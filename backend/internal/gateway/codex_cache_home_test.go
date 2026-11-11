package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// With the backend away, the list a ChatGPT sign-in is handed is made from
// Codex's cache — the one under CODEX_HOME when it is set, as Codex keeps
// it there. A cached entry restored to the account's own window keeps the
// cache's max when uni-switch's live list has none (one saved before uni-switch
// kept the max): 872000 stays, it doesn't fall to the window.
func TestCodexCachedListUnderCodexHomeKeepsItsMax(t *testing.T) {
	codexSignedIn(t)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	// uni-switch handed Codex 600000 earlier; the window is no longer set
	os.WriteFile(filepath.Join(codexHome, "models_cache.json"), []byte(`{"etag":"\"v1\"","models":[
		{"slug":"gpt-6.1-sol","priority":1,"context_window":600000,"max_context_window":872000,"base_instructions":"sol prompt"},
		{"slug":"gpt-6-astra","priority":2,"context_window":272000,"max_context_window":272000}]}`), 0o644)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()
	// the live list: one without a max, one with a max past the cache's
	catalog.SaveLive("codex", provider.CodexBase, []catalog.Model{
		{ID: "gpt-6.1-sol", Context: 272000},
		{ID: "gpt-6-astra", Context: 272000, MaxContext: 872000},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", CodexPath+"/models", nil)
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	New().Handler().ServeHTTP(rec, req)
	var got struct {
		Models []struct {
			Slug    string `json:"slug"`
			Context int    `json:"context_window"`
			Max     int    `json:"max_context_window"`
			Prompt  string `json:"base_instructions"`
		} `json:"models"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	want := map[string][2]int{"gpt-6.1-sol": {272000, 872000}, "gpt-6-astra": {272000, 872000}}
	seen := 0
	for _, m := range got.Models {
		w, ok := want[m.Slug]
		if !ok {
			continue
		}
		seen++
		if m.Context != w[0] || m.Max != w[1] {
			t.Errorf("cached %s %d/%d, want %d/%d", m.Slug, m.Context, m.Max, w[0], w[1])
		}
		if m.Slug == "gpt-6.1-sol" && m.Prompt != "sol prompt" {
			t.Errorf("cached gpt-6.1-sol prompt %q, want the cache's", m.Prompt)
		}
	}
	if seen != len(want) {
		t.Fatalf("%d of the cache's models listed, want %d (from CODEX_HOME): %s", seen, len(want), rec.Body.String())
	}
}

// A ChatGPT account's model in a catalog uni-switch writes takes its entry from
// the cache under CODEX_HOME when it is set.
func TestCodexCatalogEntryFromCodexHome(t *testing.T) {
	codexSignedIn(t)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	os.WriteFile(filepath.Join(codexHome, "models_cache.json"), []byte(`{"etag":"\"v1\"","models":[
		{"slug":"gpt-6.1-sol","context_window":272000,"max_context_window":872000,"base_instructions":"sol prompt"}]}`), 0o644)
	es := codexcat.Entries([]catalog.Model{{ID: "codex/gpt-6.1-sol", Name: "Sol", Context: 272000}}, 0)
	b, _ := json.Marshal(es[0])
	var e struct {
		Max    int    `json:"max_context_window"`
		Prompt string `json:"base_instructions"`
	}
	json.Unmarshal(b, &e)
	if e.Prompt != "sol prompt" || e.Max != 872000 {
		t.Errorf("entry max %d, prompt %.20q, want the CODEX_HOME cache's 872000 and \"sol prompt\"", e.Max, e.Prompt)
	}
}
