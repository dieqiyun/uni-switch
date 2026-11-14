package provider

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// pinmmis on X (10-29): one model from several relays, renamed to be told
// apart ("ds.4.1(WB)" for deepseek-v4.1-flash), had none of its specs, and
// there was no way to say which model it is short of typing each spec.
// The editor's Same as (ModelSameAs) now gives it that model's window,
// reply limit, levels and images wherever a model's entry is read; what
// the user set of its own still wins, and the provider's own list too.
func TestSameAsGivesAnUnlistedModelItsSpecs(t *testing.T) {
	priceHome(t)
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(`{
	  "deepseek":{"id":"deepseek","models":{"deepseek-v4.1-flash":{"id":"deepseek-v4.1-flash","limit":{"context":1048576,"output":393216},
	    "reasoning":true,"reasoning_options":[{"type":"effort","values":["high","max"]}],"modalities":{"input":["text"]},"cost":{"input":0.14,"output":0.28}}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	if err := Save(Provider{ID: "wb", Name: "WB", Key: "k", Chat: "https://wb.example/v1",
		Models: []string{"ds.4.1(WB)", "ds.4.1(own)", "deepseek-v4.1-flash [WB]"}}); err != nil {
		t.Fatal(err)
	}
	entry := func(id string) Entry {
		t.Helper()
		for _, e := range Catalog() {
			if e.ID == id {
				return e
			}
		}
		t.Fatalf("no entry %s", id)
		return Entry{}
	}
	if e := entry("wb/ds.4.1(WB)"); e.Context == 1048576 || len(e.Efforts) > 0 {
		t.Fatalf("specs before any Same as: %d %v", e.Context, e.Efforts)
	}
	// a channel's tag in brackets on an id models.dev knows: no word needed
	if e := entry("wb/deepseek-v4.1-flash [WB]"); e.Context != 1048576 || !slices.Equal(e.Efforts, []string{"high", "max"}) {
		t.Errorf("tagged id: %d %v", e.Context, e.Efforts)
	}
	same := "deepseek-v4.1-flash"
	if err := SetModelPrefs("wb", map[string]ModelPref{"ds.4.1(WB)": {Same: &same}, "ds.4.1(own)": {Same: &same}}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("wb")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetContext(*p, "ds.4.1(own)", 200000); err != nil {
		t.Fatal(err)
	}
	e := entry("wb/ds.4.1(WB)")
	if e.Context != 1048576 || e.Output != 393216 || !e.Reasoning || !slices.Equal(e.Efforts, []string{"high", "max"}) || e.Images {
		t.Errorf("after Same as: window %d, out %d, reasoning %v, levels %v, images %v", e.Context, e.Output, e.Reasoning, e.Efforts, e.Images)
	}
	if e := entry("wb/ds.4.1(own)"); e.Context != 200000 {
		t.Errorf("the user's own window lost to Same as: %d", e.Context)
	}
	// taken back: its specs go with it
	none := ""
	if err := SetModelPrefs("wb", map[string]ModelPref{"ds.4.1(WB)": {Same: &none}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().ModelSameAs["wb/ds.4.1(WB)"]; ok {
		t.Fatal("Same as kept")
	}
	if e := entry("wb/ds.4.1(WB)"); e.Context == 1048576 || len(e.Efforts) > 0 {
		t.Errorf("specs kept after Same as was taken back: %d %v", e.Context, e.Efforts)
	}
}
