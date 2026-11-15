package codexcat

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// The Codex app and its VS Code extension read the first AppList of
// model/list by priority and never the next page, hidden ones counted
// (@ZhenLuoTTAWDTT on X: "未被显示的模型也无法选择"). Fit puts a hidden
// entry after every listed one and, with more listed than AppList, brings
// the models Codex used into the first AppList, keeping the order
// otherwise.
func TestFitKeepsUsedModelsWhereTheCodexAppReads(t *testing.T) {
	var entries []any
	// Codex's own: two listed, three hidden near the top
	for i, s := range []string{"gpt-6", "codex-auto-review", "gpt-6-mini", "gpt-oss", "gpt-6-luna"} {
		v := "list"
		if i == 1 || i == 3 || i == 4 {
			v = "hide"
		}
		entries = append(entries, map[string]any{"slug": s, "priority": float64(i + 1), "visibility": v})
	}
	// uni-switch's 150, after them
	for i := range 150 {
		entries = append(entries, map[string]any{"slug": fmt.Sprintf("many/model-%03d", i), "priority": 106 + i, "visibility": "list"})
	}
	Fit(entries, []string{"many/model-140", "codex/gpt-6-luna", "many/model-003", "gone/model"})
	var order []string
	pri := map[string]int{}
	for i, e := range entries {
		o := e.(map[string]any)
		slug := o["slug"].(string)
		order = append(order, slug)
		pri[slug] = o["priority"].(int)
		if pri[slug] != i+1 {
			t.Fatalf("%s is at %d with priority %d", slug, i, pri[slug])
		}
	}
	first := order[:AppList]
	for _, s := range []string{"codex-auto-review", "gpt-oss", "gpt-6-luna"} {
		if slices.Contains(first, s) {
			t.Errorf("hidden %s is in the first %d", s, AppList)
		}
	}
	if !slices.Contains(first, "many/model-140") {
		t.Errorf("the used many/model-140 is at %d, past the first %d the Codex app reads", pri["many/model-140"], AppList)
	}
	// 2 of Codex's own listed and 98 of uni-switch's: 97 from the top, then the used one
	if want := append([]string{"gpt-6", "gpt-6-mini"}, order[2:99]...); !slices.Equal(first[:99], want) || order[98] != "many/model-096" || order[99] != "many/model-140" || order[100] != "many/model-097" {
		t.Errorf("the first 101: %v", order[:101])
	}
	if last := order[len(order)-3:]; !slices.Equal(last, []string{"codex-auto-review", "gpt-oss", "gpt-6-luna"}) {
		t.Errorf("the last three %v, want the hidden ones in their order", last)
	}
}

// A list the Codex app reads whole is left in its order, used or not.
func TestFitLeavesAShortListsOrder(t *testing.T) {
	var entries []any
	for i := range 20 {
		entries = append(entries, map[string]any{"slug": fmt.Sprintf("p/m%02d", i), "priority": i + 1, "visibility": "list"})
	}
	Fit(entries, []string{"p/m19", "p/m10"})
	for i, e := range entries {
		if s := e.(map[string]any)["slug"]; s != fmt.Sprintf("p/m%02d", i) {
			t.Fatalf("%d is %v", i, s)
		}
	}
}

// The model_catalog_json uni-switch writes Fits the same way.
func TestCatalogUsedFits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	var ms []catalog.Model
	for i := range 130 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("many/model-%03d", i)})
	}
	var got struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(CatalogUsed(ms, "", []string{"many/model-120"}), &got); err != nil {
		t.Fatal(err)
	}
	for _, m := range got.Models {
		if m["slug"] == "many/model-120" {
			if p := m["priority"].(float64); p > AppList {
				t.Fatalf("the used model's priority is %v", p)
			}
			return
		}
	}
	t.Fatal("many/model-120 is not in the catalog")
}

func TestUsedTagIsTheSet(t *testing.T) {
	if UsedTag([]string{"a", "b"}) != UsedTag([]string{"b", "a"}) {
		t.Error("the tag changed with the order alone")
	}
	if UsedTag([]string{"a"}) == UsedTag([]string{"a", "b"}) {
		t.Error("the tag didn't change with the set")
	}
}

// Codex's own models are kept in the first AppList wherever an order put
// them, ahead of the used ones; a hidden one of them still goes last
// (maiya on Discord: an order naming only uni-switch's put every GPT model
// past the 100 the Codex app reads).
func TestFitOwnKeepsCodexsOwnInTheAppsHundred(t *testing.T) {
	var entries []any
	for i := range 130 {
		entries = append(entries, map[string]any{"slug": fmt.Sprintf("many/model-%03d", i), "priority": i + 1, "visibility": "list"})
	}
	for i, s := range []string{"gpt-6-sol", "gpt-5.5", "codex-auto-review"} {
		v := "list"
		if s == "codex-auto-review" {
			v = "hide"
		}
		entries = append(entries, map[string]any{"slug": s, "priority": 131 + i, "visibility": v})
	}
	own := map[string]bool{"gpt-6-sol": true, "gpt-5.5": true, "codex-auto-review": true}
	FitOwn(entries, []string{"many/model-120"}, func(s string) bool { return own[s] })
	var order []string
	for _, e := range entries {
		order = append(order, e.(map[string]any)["slug"].(string))
	}
	first := order[:AppList]
	for _, s := range []string{"gpt-6-sol", "gpt-5.5", "many/model-120"} {
		if !slices.Contains(first, s) {
			t.Errorf("%s is at %d, past the first %d", s, slices.Index(order, s)+1, AppList)
		}
	}
	if order[len(order)-1] != "codex-auto-review" {
		t.Errorf("the hidden codex-auto-review is at %d", slices.Index(order, "codex-auto-review")+1)
	}
	// the rest in the order they stood: 97 of uni-switch's from the top
	if order[0] != "many/model-000" || order[96] != "many/model-096" || order[100] != "many/model-097" {
		t.Errorf("the first 101: %v", order[:101])
	}
}

// The model_catalog_json uni-switch writes keeps a ChatGPT account's own
// (codex/…) models in the first AppList the same way.
func TestCatalogUsedKeepsChatGPTOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	var ms []catalog.Model
	for i := range 130 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("many/model-%03d", i)})
	}
	ms = append(ms, catalog.Model{ID: "codex/gpt-5.5"})
	var got struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(CatalogUsed(ms, "", nil), &got); err != nil {
		t.Fatal(err)
	}
	for _, m := range got.Models {
		if m["slug"] == "codex/gpt-5.5" {
			if p := m["priority"].(float64); p > AppList {
				t.Fatalf("codex/gpt-5.5's priority is %v", p)
			}
			return
		}
	}
	t.Fatal("codex/gpt-5.5 is not in the catalog")
}
