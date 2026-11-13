package provider

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A provider's price rate (#819) scales the list price a call is counted
// at, as a relay charging 0.8 of the official price bills; a price the user
// set for a model is what they said it costs, and stays as set.
func TestPriceRateScalesListPrice(t *testing.T) {
	priceHome(t)
	priceCatalog(t)
	if err := Save(Provider{ID: "openai", Name: "OpenAI", Chat: "https://api.openai.com/v1", Key: "k", Models: []string{"sol"}, PriceRate: 0.5}); err != nil {
		t.Fatal(err)
	}
	pr, ok := EffectivePrice("openai", "sol")
	if !ok || pr.Input != 1 || pr.Output != 5 || pr.CacheRead != 0.125 || pr.CacheWrite != 1.25 {
		t.Fatalf("at half the list price: %+v %v", pr, ok)
	}
	if err := settings.Save(settings.Settings{ModelPrices: map[string]settings.ModelPrice{"openai/sol": statePrice(3)}}); err != nil {
		t.Fatal(err)
	}
	if pr, ok := EffectivePrice("openai", "sol"); !ok || pr.Input != 3 || pr.Output != 3 {
		t.Fatalf("a price set for the model is not scaled: %+v %v", pr, ok)
	}
}

func TestPriceRateOK(t *testing.T) {
	for _, r := range []float64{0, 0.8, 1.5, 0.125, 1000} {
		if bad := PriceRateOK(r); bad != "" {
			t.Errorf("%v: %s", r, bad)
		}
	}
	for _, r := range []float64{-1, 1000.5, 0.1234} {
		if PriceRateOK(r) == "" {
			t.Errorf("%v taken", r)
		}
	}
}

// A signed-in account's price rate is kept and counted, as a key
// provider's is (PaulT on Discord: Codex's 价格倍率 wasn't saved). Save
// kept only an account's picks and limits, and withPicks read none back,
// so the editor's Save wrote nothing and the field came back empty.
func TestAccountPriceRateKept(t *testing.T) {
	signIn(t)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	priceCatalog(t)
	for _, id := range []string{"codex", "copilot"} {
		before, ok := EffectivePrice(id, "sol")
		if !ok {
			t.Fatalf("%s: sol has no price", id)
		}
		p, err := Find(id)
		if err != nil {
			t.Fatal(err)
		}
		q := *p
		q.PriceRate = 0.5
		if err := Save(q); err != nil {
			t.Fatal(err)
		}
		if p, err := Find(id); err != nil || p.PriceRate != 0.5 {
			t.Fatalf("%s: rate after Save: %+v %v", id, p, err)
		}
		if after, ok := EffectivePrice(id, "sol"); !ok || after.Input != before.Input*0.5 || after.Output != before.Output*0.5 {
			t.Fatalf("%s: %+v at the rate, %+v before", id, after, before)
		}
		// cleared, it is the official price again
		q.PriceRate = 0
		if err := Save(q); err != nil {
			t.Fatal(err)
		}
		if p, _ := Find(id); p.PriceRate != 0 {
			t.Fatalf("%s: rate left after clearing: %v", id, p.PriceRate)
		}
	}
}
