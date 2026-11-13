package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// #2344 (liuweifeng): a provider's own id that no list prices (WorkBuddy's
// kimi-k3-1, a plugin's qfmodel) was unpriced even when the user had said
// which model it is (Same as, ModelSameAs). Its calls are costed at that
// model's maker's list price, at the provider's price rate; the user's own
// price and the provider's list still come first, and a model with no Same
// as is never matched by its name.
func TestSameAsPricesAnUnlistedModel(t *testing.T) {
	priceHome(t)
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(`{
	  "moonshotai":{"id":"moonshotai","models":{"kimi-k3":{"id":"kimi-k3","cost":{"input":0.6,"output":2.5,"cache_read":0.15}}}},
	  "relaycat":{"id":"relaycat","models":{"listed":{"id":"listed","cost":{"input":9,"output":9}}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	if err := Save(Provider{ID: "wb", Name: "WB", Key: "k", Chat: "https://wb.example/v1",
		Models: []string{"kimi-k3-1", "qfmodel", "listed", "other"}, Catalog: "relaycat", PriceRate: 0.5}); err != nil {
		t.Fatal(err)
	}
	if pr, ok := EffectivePrice("wb", "kimi-k3-1"); ok {
		t.Fatalf("priced before any Same as: %+v", pr)
	}
	s := settings.Load()
	s.ModelSameAs = map[string]string{
		"wb/kimi-k3-1":   "kimi-k3",
		"wb/qfmodel":     "moonshotai/kimi-k3", // a vendor's prefix
		"wb/listed":      "kimi-k3",
		"gone/kimi-k3-1": "kimi-k3",
	}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		pid, model string
		in         float64
	}{
		{"wb", "kimi-k3-1", 0.3}, // Kimi K3's $0.6, at the rate
		{"wb", "qfmodel", 0.3},
		{"wb", "listed", 4.5}, // its provider's own list comes first
		{"gone", "kimi-k3-1", 0.6},
	} {
		if pr, ok := EffectivePrice(c.pid, c.model); !ok || pr.Input != c.in {
			t.Errorf("EffectivePrice(%q, %q) = %+v, %v; want $%v in", c.pid, c.model, pr, ok, c.in)
		}
	}
	if pr, ok := EffectivePrice("wb", "other"); ok {
		t.Errorf("a model with no Same as was priced: %+v", pr)
	}
	// the user's own price for it still wins
	s = settings.Load()
	s.ModelPrices = map[string]settings.ModelPrice{"wb/kimi-k3-1": statePrice(1)}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if pr, _ := EffectivePrice("wb", "kimi-k3-1"); pr.Input != 1 {
		t.Errorf("the Same as price beat the user's own: %+v", pr)
	}
}
