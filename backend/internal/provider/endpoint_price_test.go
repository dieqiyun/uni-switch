package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// useEndpointCatalog puts testdata/endpoint_catalog.json in the catalog
// cache: models.dev's amd (2026-10-14, as #2052's reporter read it), whose
// "api" is AMD Radeon Cloud's endpoint and which lists Qwen3.8-27B at $0;
// alibaba, its maker, which sells no qwen3.8-27b; alibaba-coding-plan, a
// plan listing qwen3.5-plus at $0; minimax and minimax-coding-plan, which
// share one "api"; thinkingmachines, alone at an Anthropic API's; and
// openai's gpt-5.5. Id, name, api, cost and canonical
// id of each, as models.dev lists them.
func useEndpointCatalog(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	b, err := os.ReadFile(filepath.Join("testdata", "endpoint_catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
}

const amdEndpoint = "https://developer.amd.com.cn/radeon/api/v1"

// #2052 (mengyianx): a custom provider at a vendor's own endpoint is priced
// from the catalog models.dev lists at that endpoint, whatever the provider
// is called: Qwen3.8-27B at AMD's is $0, a price, where its maker sells no
// such model and it read as no price at all.
func TestEndpointCatalogPrices(t *testing.T) {
	useEndpointCatalog(t)
	for _, c := range []struct{ id, chat string }{
		{"002", amdEndpoint}, {"amd", amdEndpoint}, {"001", amdEndpoint},
		// as pasted: a trailing slash, another case
		{"002", amdEndpoint + "/"}, {"002", "https://Developer.AMD.com.cn/radeon/api/v1"},
	} {
		p := normalize(Provider{ID: c.id, Name: c.id, Key: "k", Chat: c.chat})
		if got := p.EndpointCatalog(); got != "amd" {
			t.Errorf("%s at %s: EndpointCatalog() = %q, want amd", c.id, c.chat, got)
		}
		if pr, ok := p.ListPrice("Qwen3.8-27B"); !ok || !pr.Same(catalog.Price{}) {
			t.Errorf("%s at %s: ListPrice(Qwen3.8-27B) = %+v %v, want $0, priced", c.id, c.chat, pr, ok)
		}
		// a model the endpoint prices, at its price there
		want := catalog.Price{Input: 0.14, Output: 0.28, CacheRead: 0.0028}
		if pr, ok := p.ListPrice("DeepSeek-V4.1-Flash"); !ok || !pr.Same(want) {
			t.Errorf("%s at %s: ListPrice(DeepSeek-V4.1-Flash) = %+v %v, want %+v", c.id, c.chat, pr, ok, want)
		}
	}

	// an Anthropic API's: uni-switch keeps its base as its root, models.dev
	// writes it with its /v1
	tm := normalize(Provider{ID: "tm", Key: "k", Anthropic: "https://tinker.thinkingmachines.dev/services/tinker-prod/anthropic/api/v1"})
	inkling := catalog.Price{Input: 1.87, Output: 4.68, CacheRead: 0.374}
	if pr, ok := tm.ListPrice("thinkingmachines/Inkling"); tm.EndpointCatalog() != "thinkingmachines" || !ok || !pr.Same(inkling) {
		t.Errorf("at an Anthropic endpoint: %q, %+v %v, want thinkingmachines at %+v", tm.EndpointCatalog(), pr, ok, inkling)
	}

	// what `uni-switch model price 002/Qwen3.8-27B` and the Usage page read
	if err := Save(normalize(Provider{ID: "002", Name: "002", Key: "k", Chat: amdEndpoint})); err != nil {
		t.Fatal(err)
	}
	if pr, ok := EffectivePrice("002", "Qwen3.8-27B"); !ok || !pr.Same(catalog.Price{}) {
		t.Errorf("EffectivePrice(002, Qwen3.8-27B) = %+v %v, want $0, priced", pr, ok)
	}
	// the user's own price still comes first
	user := catalog.Price{Input: 0.3, Output: 1.2, CacheRead: 0.05, CacheWrite: 0.2}
	s := settings.Settings{ModelPrices: map[string]settings.ModelPrice{
		"002/Qwen3.8-27B": {Input: &user.Input, Output: &user.Output, CacheRead: &user.CacheRead, CacheWrite: &user.CacheWrite}}}
	if pr, ok := EffectivePriceIn(s, "002", "Qwen3.8-27B"); !ok || !pr.Same(user) {
		t.Errorf("with the user's price: %+v %v, want %+v", pr, ok, user)
	}
}

// #2052: where models.dev names no single catalog for the endpoint, or the
// user named one, nothing changes: a relay's model its maker doesn't sell
// stays unpriced, never priced at a reseller's guess.
func TestEndpointCatalogOnlyItsOwn(t *testing.T) {
	useEndpointCatalog(t)
	qwen35 := catalog.Price{Input: 0.4, Output: 2.4}
	gpt := catalog.Price{Input: 5, Output: 30, CacheRead: 0.5}
	minimax := catalog.Price{Input: 0.3, Output: 1.2, CacheRead: 0.06}
	for _, c := range []struct {
		name  string
		p     Provider
		model string
		want  *catalog.Price // nil: no price
	}{
		{"a relay's endpoint", Provider{ID: "001", Key: "k", Chat: "https://relay.example.com/v1"}, "Qwen3.8-27B", nil},
		{"a relay's endpoint, a model its maker sells", Provider{ID: "001", Key: "k", Chat: "https://relay.example.com/v1"}, "gpt-5.5", &gpt},
		// the user's catalog stands: alibaba has no qwen3.8-27b
		{"a catalog the user named", Provider{ID: "002", Key: "k", Chat: amdEndpoint, Catalog: "alibaba"}, "Qwen3.8-27B", nil},
		// a plan's $0 is its membership's, not the model's: its maker's
		{"a plan's endpoint", Provider{ID: "cp", Key: "k", Chat: "https://coding-intl.dashscope.aliyuncs.com/v1"}, "qwen3.5-plus", &qwen35},
		// two catalogs at one endpoint, the plan's $0 among them: which
		// one isn't known, so its maker's (an Anthropic base is kept as its
		// root, models.dev's has its /v1)
		{"an endpoint two catalogs share", Provider{ID: "mm", Key: "k", Anthropic: "https://api.minimax.io/anthropic/v1"}, "MiniMax-M3", &minimax},
		// a subscription's account is priced as its own code says
		{"a subscription's account", Provider{ID: "sub", Chat: amdEndpoint, Account: &Account{Agent: "codex", User: "u"}}, "Qwen3.8-27B", nil},
	} {
		p := normalize(c.p)
		if c.name != "a catalog the user named" && p.EndpointCatalog() != "" {
			t.Errorf("%s: EndpointCatalog() = %q, want none", c.name, p.EndpointCatalog())
		}
		pr, ok := p.ListPrice(c.model)
		if !ok {
			pr, ok = MakerPrice(c.model)
		}
		switch {
		case c.want == nil && ok:
			t.Errorf("%s: %s priced at %+v, want no price", c.name, c.model, pr)
		case c.want != nil && (!ok || !pr.At(0).Same(*c.want)):
			t.Errorf("%s: %s = %+v %v, want %+v", c.name, c.model, pr, ok, *c.want)
		}
	}
}
