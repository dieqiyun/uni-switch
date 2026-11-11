package agent

import (
	"os"
	"reflect"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2380: Trae CN's kimi-k3-max thinks with no levels uni-switch knows, and
// /v1/models said reasoning: true for it, but Pi's models.json was written
// reasoning: false (the export read the levels alone), so Pi offered off
// alone and `uni-switch pi effort high` was clamped to off. Every export that
// writes a reasoning flag says what /v1/models says.
func TestThinkingWithoutLevelsReachesTheExports(t *testing.T) {
	syncHome(t)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{
	  "moonshotai": {"models": {"kimi-k3-max": {"id":"kimi-k3-max","name":"Kimi-K3-Max","reasoning":true,"limit":{"context":262144}}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	if err := provider.Save(provider.Provider{ID: "trae-cn", Name: "Trae CN", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Catalog: "moonshotai", Models: []string{"kimi-k3-max"}}); err != nil {
		t.Fatal(err)
	}
	const id = "trae-cn/kimi-k3-max"

	shown, _ := provider.CatalogFor("pi")
	var entry *provider.Entry
	for i := range shown {
		if shown[i].ID == id {
			entry = &shown[i]
		}
	}
	if entry == nil || !entry.Thinks() || len(entry.Efforts) != 0 {
		t.Fatalf("the fixture is a model that thinks with no levels, as /v1/models says it: %+v", entry)
	}

	if got := piOffered("pi", "uni-switch/"+id); !reflect.DeepEqual(got, []string{"off", "minimal", "low", "medium", "high"}) {
		t.Fatalf("Pi offers %v for %s, want its own off to high", got, id)
	}
	if got := piClamp("high", piOffered("pi", "uni-switch/"+id)); got != "high" {
		t.Fatalf("uni-switch pi effort high runs at %q", got)
	}

	ms := ccProviderJSONAt("http://127.0.0.1:1/v1").(map[string]any)["models"].(map[string]any)
	cc, _ := ms[id].(map[string]any)
	if cc["reasoning"] != true {
		t.Fatalf("Command Code's entry: %v", cc)
	}
	if _, set := cc["reasoningEfforts"]; set {
		t.Fatalf("Command Code was told levels uni-switch doesn't know: %v", cc)
	}
}
