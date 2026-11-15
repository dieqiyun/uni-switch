package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Codex's /model lists the models in the order dragged on the Agents page
// (M3chD09, #855): the account's own and uni-switch's in one order, a model
// it doesn't name after them as it stood, and the ETag changes with the
// order so Codex asks for the list again. Without one, the backend's come
// first, as before.
func TestCodexModelListInUsersOrder(t *testing.T) {
	codexSignedIn(t)
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-5.5", "gpt-5-codex"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: "http://127.0.0.1:1",
		Models: []string{"claude-opus-5.5", "claude-sonnet-4-5"}}); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		io.WriteString(w, `{"models":[{"slug":"gpt-5.5","priority":1},{"slug":"gpt-5-codex","priority":2}]}`)
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()
	// the slugs as Codex lists them, by priority, and the list's ETag
	list := func() ([]string, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", CodexPath+"/models", nil)
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		New().Handler().ServeHTTP(rec, req)
		var got struct {
			Models []map[string]any `json:"models"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		slices.SortStableFunc(got.Models, func(a, b map[string]any) int {
			return int(a["priority"].(float64) - b["priority"].(float64))
		})
		var out []string
		for _, m := range got.Models {
			out = append(out, m["slug"].(string))
		}
		return out, rec.Header().Get("ETag")
	}
	before, tag := list()
	if want := []string{"gpt-5.5", "gpt-5-codex", "relay/claude-opus-5.5", "relay/claude-sonnet-4-5"}; !slices.Equal(before, want) {
		t.Fatalf("default order %v, want %v", before, want)
	}
	var own string
	for _, e := range provider.Catalog() {
		if provider.CodexOwn(e) && e.Model == "gpt-5-codex" {
			own = e.ID
		}
	}
	if own == "" {
		t.Fatal("no catalog entry for the account's gpt-5-codex")
	}
	// uni-switch's sonnet first, then the account's gpt-5-codex; the rest after
	if err := provider.SetModelOrder("codex", []string{"relay/claude-sonnet-4-5", own}); err != nil {
		t.Fatal(err)
	}
	after, tag2 := list()
	if want := []string{"relay/claude-sonnet-4-5", "gpt-5-codex", "gpt-5.5", "relay/claude-opus-5.5"}; !slices.Equal(after, want) {
		t.Errorf("ordered %v, want %v", after, want)
	}
	if tag2 == tag {
		t.Errorf("the ETag %q didn't change with the order", tag)
	}
	// the catalog uni-switch writes for Codex follows it too
	shown, _ := provider.CatalogFor("codex")
	var ids []string
	for _, e := range shown {
		ids = append(ids, e.ID)
	}
	if len(ids) < 2 || ids[0] != "relay/claude-sonnet-4-5" || ids[1] != own {
		t.Errorf("CatalogFor(codex) %v", ids)
	}
	// another agent's list keeps uni-switch's own order
	other, _ := provider.CatalogFor("opencode")
	for i, e := range other {
		if e.ID == "relay/claude-sonnet-4-5" && i == 0 {
			t.Errorf("opencode's list took Codex's order: %v", other)
		}
	}
	// none puts the backend's first again
	if err := provider.SetModelOrder("codex", nil); err != nil {
		t.Fatal(err)
	}
	if back, _ := list(); !slices.Equal(back, before) {
		t.Errorf("after the order was taken away %v, want %v", back, before)
	}
}
