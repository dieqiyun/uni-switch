package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// useXiaomiCatalog puts testdata/xiaomi_catalog.json in the catalog cache:
// models.dev's xiaomi and its three Token Plan catalogs (2026-10-13; id,
// name, cost and canonical id of each model), the plans listing every model
// at $0; plus a relay's catalog that lists mimo-v2.6-flash at $0 of its own.
func useXiaomiCatalog(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	b, err := os.ReadFile(filepath.Join("testdata", "xiaomi_catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["freebie"] = map[string]any{"id": "freebie", "models": map[string]any{
		"mimo-v2.6-flash": map[string]any{"id": "mimo-v2.6-flash", "cost": map[string]any{"input": 0, "output": 0}}}}
	b, _ = json.Marshal(m)
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
}

// xiaomiAt is a saved endpoint fixture with an explicit model catalog.
func xiaomiAt(t *testing.T, region, cat string) Provider {
	t.Helper()
	host := "api.xiaomimimo.com"
	if strings.HasPrefix(region, "plan-") {
		host = "token-" + region + ".xiaomimimo.com"
	}
	return normalize(Provider{ID: "xiaomi", Name: "Xiaomi MiMo", Preset: "xiaomi", Key: "k",
		Chat: "https://" + host + "/v1", Catalog: cat})
}

var (
	mimoFlash = catalog.Price{Input: 0.14, Output: 0.28, CacheRead: 0.0028}
	mimoPro   = catalog.Price{Input: 0.435, Output: 0.87, CacheRead: 0.0036}
)

// #1873: a Xiaomi MiMo call is priced at xiaomi's own list price, at every
// region: the Token Plans' catalogs, which list every model at $0 and came
// first in the preset's catalog, are not read for prices.
func TestXiaomiListPrice(t *testing.T) {
	useXiaomiCatalog(t)
	oldDefault := "xiaomi-token-plan-cn, xiaomi"
	for _, c := range []struct{ region, cat string }{
		{"api", oldDefault}, {"api", "xiaomi"},
		{"plan-cn", oldDefault}, {"plan-sgp", oldDefault}, {"plan-ams", oldDefault},
		// a catalog the user named: the plan's alone, or the plan's first
		{"api", "xiaomi-token-plan-sgp"}, {"plan-ams", "openrouter, xiaomi-token-plan-ams, xiaomi"},
	} {
		p := xiaomiAt(t, c.region, c.cat)
		for model, want := range map[string]catalog.Price{"mimo-v2.6-flash": mimoFlash, "mimo-v2.6-pro": mimoPro} {
			if pr, ok := p.ListPrice(model); !ok || !pr.Same(want) {
				t.Errorf("%s at %q, %s: %+v %v, want %+v", c.region, p.Catalog, model, pr, ok, want)
			}
		}
	}
	// with the provider gone, its maker's price: not the plan's $0
	if pr, ok := MakerPrice("mimo-v2.6-flash"); !ok || !pr.Same(mimoFlash) {
		t.Errorf("MakerPrice(mimo-v2.6-flash) = %+v %v, want %+v", pr, ok, mimoFlash)
	}
	// a catalog listing the model at $0 ahead of one with its price is
	// still read first: a relay's own free model stays free
	relay := Provider{ID: "relay", Catalog: "freebie, xiaomi"}
	if pr, ok := relay.ListPrice("mimo-v2.6-flash"); !ok || !pr.Same(catalog.Price{}) {
		t.Errorf("relay mimo-v2.6-flash: %+v %v, want its own $0", pr, ok)
	}
	// a Xiaomi provider isn't a Kimi Code one: its ids aren't Kimi's
	if xiaomiAt(t, "plan-cn", oldDefault).kimiCodeMember() {
		t.Error("a Xiaomi Token Plan provider counts as a Kimi Code membership")
	}

	// what `uni-switch model price` and the Usage page read: the saved provider's
	p := xiaomiAt(t, "api", oldDefault)
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if pr, ok := EffectivePrice("xiaomi", "mimo-v2.6-flash"); !ok || !pr.Same(mimoFlash) {
		t.Errorf("EffectivePrice(xiaomi, mimo-v2.6-flash) = %+v %v, want %+v", pr, ok, mimoFlash)
	}
}
