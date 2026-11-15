package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/codexcat"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// The Codex app and its VS Code extension show the first 100 of Codex's
// model list by priority, hidden ones counted, and never ask for the rest
// (@ZhenLuoTTAWDTT on X: with uni-switch's models in it, the ones past that
// couldn't be shown or chosen). The list uni-switch hands Codex puts the
// account's hidden models last and brings the ones the user used into the
// first 100; its ETag changes when Codex uses another, so Codex asks again.
func TestCodexModelListKeepsUsedModelsInTheAppsHundred(t *testing.T) {
	codexSignedIn(t)
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	var ms []string
	for i := range 130 {
		ms = append(ms, fmt.Sprintf("model-%03d", i))
	}
	if err := provider.Save(provider.Provider{ID: "many", Name: "Many", Key: "k", Chat: "http://127.0.0.1:9/v1", Models: ms}); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		io.WriteString(w, `{"models":[{"slug":"codex-auto-review","priority":0,"visibility":"hide"},{"slug":"gpt-5.5","priority":1,"visibility":"list"}]}`)
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()
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
	usage.Append(usage.Record{Time: time.Now().Add(-time.Hour), Agent: "codex", Provider: "many", Requested: "many/model-120", Status: 200})
	got, tag := list()
	if len(got) != 132 {
		t.Fatalf("%d models", len(got))
	}
	first := got[:codexcat.AppList]
	if slices.Contains(first, "codex-auto-review") || got[len(got)-1] != "codex-auto-review" {
		t.Errorf("the hidden codex-auto-review is at %d of %d", slices.Index(got, "codex-auto-review")+1, len(got))
	}
	if !slices.Contains(first, "many/model-120") {
		t.Errorf("the used many/model-120 is at %d, past the %d the Codex app reads", slices.Index(got, "many/model-120")+1, codexcat.AppList)
	}
	if first[0] != "gpt-5.5" || first[1] != "many/model-000" {
		t.Errorf("the top is %v", first[:2])
	}
	usage.Append(usage.Record{Time: time.Now(), Agent: "codex", Provider: "many", Requested: "many/model-125", Status: 200})
	got, tag2 := list()
	if !slices.Contains(got[:codexcat.AppList], "many/model-125") {
		t.Errorf("the newly used many/model-125 is at %d", slices.Index(got, "many/model-125")+1)
	}
	if tag2 == tag {
		t.Errorf("the ETag %q didn't change with the models used", tag)
	}
}

// The catalog a Codex on another computer reads puts the models that key's
// Codex used in the first 100, and no other key's. Codex 0.166 reads the
// catalog whole; an older one is held to 1 MiB, about 48 entries
// (TestCodexCatalogFitsCodexsLimit).
func TestCodexCatalogKeepsTheKeysUsedModelsFirst(t *testing.T) {
	fresh(t)
	var ms []string
	for i := range 130 {
		ms = append(ms, fmt.Sprintf("model-%03d", i))
	}
	if err := provider.Save(provider.Provider{ID: "many", Name: "Many", Key: "k", Chat: "http://127.0.0.1:9/v1", Models: ms}); err != nil {
		t.Fatal(err)
	}
	usage.Append(usage.Record{Time: time.Now().Add(-time.Hour), Agent: "codex", Provider: "many", Requested: "many/model-110", Status: 200})
	usage.Append(usage.Record{Time: time.Now().Add(-time.Hour), Agent: "codex", Provider: "many", Requested: "many/model-111", CallerKeyID: "someone-else", Status: 200})
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, httptest.NewRequest("GET", CodexCatalogPath+"?client_version=0.166.0", nil))
	var list struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	at := map[string]float64{}
	for _, m := range list.Models {
		at[m["slug"].(string)] = m["priority"].(float64)
	}
	if p := at["many/model-110"]; p == 0 || p > codexcat.AppList {
		t.Errorf("the used many/model-110's priority is %v", p)
	}
	if p := at["many/model-111"]; p <= codexcat.AppList {
		t.Errorf("another key's many/model-111 was brought up, to %v", p)
	}
}

// maiya (Discord): after an update the Codex app showed uni-switch's models
// and none of Codex's own GPT ones. With an order of Codex's models set on
// the Agents page while its list had no Codex section (the codex provider
// kept for routing groups before a3ee00c05, Codex not signed in yet, or a
// GPT model OpenAI added since), the order names 100 or more of uni-switch's
// and none of the account's, so Order put every own one after them, past
// the 100 the Codex app reads. The account's own listed models are kept
// in the first 100, wherever the order put them; a hidden one stays last.
func TestCodexModelListKeepsOwnModelsInTheAppsHundred(t *testing.T) {
	codexSignedIn(t)
	var ms, order []string
	for i := range 130 {
		ms = append(ms, fmt.Sprintf("model-%03d", i))
		order = append(order, fmt.Sprintf("many/model-%03d", i))
	}
	if err := provider.Save(provider.Provider{ID: "many", Name: "Many", Key: "k", Chat: "http://127.0.0.1:9/v1", Models: ms}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelOrder("codex", order); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		io.WriteString(w, `{"models":[{"slug":"codex-auto-review","priority":0,"visibility":"hide"},{"slug":"gpt-6-sol","priority":1,"visibility":"list"},{"slug":"gpt-5.5","priority":2,"visibility":"list"}]}`)
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()
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
	var order2 []string
	for _, m := range got.Models {
		order2 = append(order2, m["slug"].(string))
	}
	if len(order2) != 133 {
		t.Fatalf("%d models", len(order2))
	}
	first := order2[:codexcat.AppList]
	for _, s := range []string{"gpt-6-sol", "gpt-5.5"} {
		if !slices.Contains(first, s) {
			t.Errorf("Codex's own %s is at %d, past the %d the Codex app reads", s, slices.Index(order2, s)+1, codexcat.AppList)
		}
	}
	if order2[len(order2)-1] != "codex-auto-review" {
		t.Errorf("the hidden codex-auto-review is at %d of %d", slices.Index(order2, "codex-auto-review")+1, len(order2))
	}
	// the user's order holds for the rest: uni-switch's first 98 as they put them
	if !slices.Equal(first[:98], order[:98]) {
		t.Errorf("the first 98: %v", first[:98])
	}
}
