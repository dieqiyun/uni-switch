package provider

import (
	"fmt"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A providers.json from before #1818 has no models key: the vendor's
// list is served as ever, up to manyModels.
func TestNoPicksStillServesTheVendorList(t *testing.T) {
	azureHome(t)
	var ms []catalog.Model
	for i := range manyModels + 6 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("m%d", i)})
	}
	p := Provider{ID: "old", Name: "old", Key: "sk-x", Chat: "https://old.example.invalid/v1"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("old", p.Chat, ms); err != nil {
		t.Fatal(err)
	}
	got, err := Find("old")
	if err != nil {
		t.Fatal(err)
	}
	if got.NoModels || len(got.Exposed()) != manyModels {
		t.Errorf("noModels %v exposed %d, want %d", got.NoModels, len(got.Exposed()), manyModels)
	}
}
