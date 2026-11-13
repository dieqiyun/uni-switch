package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A Grok id named at an effort is priced as its model; a fast, mini or
// build one no catalog lists stays unpriced (#224).
func TestGrokEffortListPrice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "xai":{"id":"xai","models":{
	    "grok-4.7":{"id":"grok-4.7","cost":{"input":2,"output":6,"cache_read":0.5}},
	    "grok-4.6":{"id":"grok-4.6","cost":{"input":2,"output":6,"cache_read":0.5}}}},
	  "openai":{"id":"openai","models":{
	    "gpt-6-astra":{"id":"gpt-6-astra","cost":{"input":10,"output":50}}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	grok := Provider{ID: "grok", Name: "Grok (SuperGrok)"}
	for _, id := range []string{"grok-4.7", "grok-4.7-low", "grok-4.7-medium", "grok-4.7-high", "grok-4.7-xhigh",
		"GROK-4.7-High", "x-ai/grok-4.7-xhigh", "grok-4.6-minimal",
		// a model at a window of its own (#1608)
		"grok-4.7@500k", "grok-4.6@500k"} {
		for name, price := range map[string]func(string) (catalog.Price, bool){"MakerPrice": MakerPrice, "ListPrice": grok.ListPrice} {
			pr, ok := price(id)
			if !ok || pr.Input != 2 || pr.Output != 6 || pr.CacheRead != 0.5 {
				t.Errorf("%s(%q) = %+v, %v; want grok's $2/$6/$0.5", name, id, pr, ok)
			}
		}
	}
	for _, id := range []string{"grok-4.7-fast", "grok-4.7-low-fast", "grok-4.7-xhigh-fast", "grok-4.7-build",
		"grok-4.7-build-fast", "grok-4.7-mini", "grok-4.7-mini-high", "grok-4.7-max",
		// only Grok's ids are read this way
		"gpt-6-astra-high", "cursor-grok-4.7-high", "grok-4.7-build@500k"} {
		if pr, ok := grok.ListPrice(id); ok {
			t.Errorf("ListPrice(%q) = %+v; want unpriced", id, pr)
		}
		if pr, ok := MakerPrice(id); ok {
			t.Errorf("MakerPrice(%q) = %+v; want unpriced", id, pr)
		}
	}
	// a window is read off any id, not only Grok's: the Cursor plugin's
	// sized ids (TestCursorWindowIDsPricedAsTheirModel)
	if pr, ok := grok.ListPrice("gpt-6-astra@500k"); !ok || pr.Input != 10 || pr.Output != 50 {
		t.Errorf("ListPrice(gpt-6-astra@500k) = %+v, %v; want gpt-6-astra's $10/$50", pr, ok)
	}
	if got := PricedName("grok-4.7-xhigh"); got != "grok-4.7" {
		t.Errorf("PricedName(grok-4.7-xhigh) = %q", got)
	}
	if got := PricedName("grok-4.7-low-fast"); got != "grok-4.7-low-fast" {
		t.Errorf("PricedName(grok-4.7-low-fast) = %q", got)
	}
}

// A window the id names without a k (the grok plugin writes a window that
// no multiple of 1000 is as bare digits) is priced as its model, as one
// spelled with a k is.
func TestGrokWindowWithoutKListPrice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "xai":{"id":"xai","models":{
	    "grok-4.7":{"id":"grok-4.7","cost":{"input":2,"output":6,"cache_read":0.5}}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	grok := Provider{ID: "grok", Name: "Grok (SuperGrok)"}
	for _, id := range []string{"grok-4.7@131072", "x-ai/grok-4.7@131072"} {
		if pr, ok := grok.ListPrice(id); !ok || pr.Input != 2 || pr.Output != 6 || pr.CacheRead != 0.5 {
			t.Errorf("ListPrice(%q) = %+v, %v; want grok-4.7's $2/$6/$0.5", id, pr, ok)
		}
		if pr, ok := MakerPrice(id); !ok || pr.Input != 2 {
			t.Errorf("MakerPrice(%q) = %+v, %v; want grok-4.7's", id, pr, ok)
		}
	}
}
