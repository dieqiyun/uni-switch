package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The windows the user set on the codex provider reach Codex's own entries
// in the list a ChatGPT sign-in is handed, as /v1/models says them (#674):
// the window, and the max where the window is past it; the rest of each
// entry as the backend gave it, and a model with none set as it was.
// Taking one away changes the ETag and the window goes back, also in a list
// made from Codex's cache, which keeps what uni-switch handed it.
func TestCodexNativeModelsTakeSetWindows(t *testing.T) {
	codexSignedIn(t)
	forgetCodexLists(t)
	if err := provider.Save(provider.Provider{ID: "codex", Contexts: map[string]int{"gpt-6.1-sol": 600000, "gpt-5.5": 400000}}); err != nil {
		t.Fatal(err)
	}
	const backend = `{"models":[
		{"slug":"gpt-6.1-sol","priority":1,"context_window":272000,"max_context_window":872000,"base_instructions":"sol prompt","effective_context_window_percent":95},
		{"slug":"gpt-6-astra","priority":2,"context_window":272000,"max_context_window":872000},
		{"slug":"gpt-5.5","priority":3,"context_window":272000,"max_context_window":272000}]}`
	down := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		io.WriteString(w, backend)
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()

	list := func() (map[string]map[string]any, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", CodexPath+"/models", nil)
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		New().Handler().ServeHTTP(rec, req)
		var got struct {
			Models []map[string]any `json:"models"`
		}
		json.Unmarshal(rec.Body.Bytes(), &got)
		out := map[string]map[string]any{}
		for _, m := range got.Models {
			s, _ := m["slug"].(string)
			out[s] = m
		}
		return out, rec.Header().Get("ETag")
	}
	window := func(m map[string]any) (int, int) {
		c, _ := m["context_window"].(float64)
		x, _ := m["max_context_window"].(float64)
		return int(c), int(x)
	}

	got, set := list()
	if c, x := window(got["gpt-6.1-sol"]); c != 600000 || x != 872000 {
		t.Errorf("gpt-6.1-sol %d/%d, want 600000/872000", c, x)
	}
	if got["gpt-6.1-sol"]["base_instructions"] != "sol prompt" || got["gpt-6.1-sol"]["effective_context_window_percent"] != float64(95) {
		t.Errorf("the backend's entry changed: %v", got["gpt-6.1-sol"])
	}
	if c, x := window(got["gpt-6-astra"]); c != 272000 || x != 872000 {
		t.Errorf("gpt-6-astra %d/%d, want the backend's 272000/872000", c, x)
	}
	if c, x := window(got["gpt-5.5"]); c != 400000 || x != 400000 {
		t.Errorf("gpt-5.5 %d/%d, want 400000 with the max raised to it", c, x)
	}

	// Codex keeps that list in its cache; the window is taken away
	home, _ := os.UserHomeDir()
	cache := map[string]any{"etag": set, "models": []any{got["gpt-6.1-sol"], got["gpt-6-astra"], got["gpt-5.5"]}}
	b, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), b, 0o644)
	p, err := provider.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DropContext(*p, "gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DropContext(*p, "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	got, reset := list()
	if reset == set {
		t.Errorf("ETag %q unchanged after the window was taken away", reset)
	}
	if c, _ := window(got["gpt-6.1-sol"]); c != 272000 {
		t.Errorf("gpt-6.1-sol %d after the reset, want 272000", c)
	}

	// the backend away: the cache stands in, with the account's own windows
	catalog.SaveLive("codex", provider.CodexBase, []catalog.Model{
		{ID: "gpt-6.1-sol", Context: 272000, MaxContext: 872000},
		{ID: "gpt-5.5", Context: 272000},
	})
	down = true
	forgetCodexLists(t) // its list, kept codexListFor, gone too
	got, _ = list()
	if c, x := window(got["gpt-6.1-sol"]); c != 272000 || x != 872000 {
		t.Errorf("cached gpt-6.1-sol %d/%d, want 272000/872000", c, x)
	}
	// the live list has no max for it: the cache's own stays
	if c, x := window(got["gpt-5.5"]); c != 272000 || x != 400000 {
		t.Errorf("cached gpt-5.5 %d/%d, want 272000/400000", c, x)
	}
}

// A ChatGPT account's model in a catalog uni-switch writes (model_catalog_json)
// takes its entry from Codex's cache under uni-switch's id, with the window
// uni-switch resolved for it, not the one the cache kept (#674).
func TestCodexCatalogOwnEntryTakesResolvedWindow(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"etag":"\"v1\"","models":[
		{"slug":"gpt-6.1-sol","context_window":600000,"max_context_window":872000,"base_instructions":"sol prompt"}]}`), 0o644)
	window := func(n int) (int, int, any) {
		es := codexcat.Entries([]catalog.Model{{ID: "codex/gpt-6.1-sol", Name: "Sol", Context: n}}, 0)
		// as Codex reads it
		b, _ := json.Marshal(es[0])
		var e struct {
			Context int    `json:"context_window"`
			Max     int    `json:"max_context_window"`
			Prompt  string `json:"base_instructions"`
		}
		json.Unmarshal(b, &e)
		return e.Context, e.Max, e.Prompt
	}
	if c, x, p := window(272000); c != 272000 || x != 872000 || p != "sol prompt" {
		t.Errorf("%d/%d %v, want 272000/872000 with the entry's prompt", c, x, p)
	}
	if c, x, _ := window(900000); c != 900000 || x != 900000 {
		t.Errorf("%d/%d, want 900000 with the max raised to it", c, x)
	}
}
