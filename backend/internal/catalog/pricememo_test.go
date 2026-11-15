package catalog

import "testing"

// TestPricedOnceALoad: a model priced again at the same load is not
// looked for again (the GUI's provider list prices every model on each
// poll), and a Reset to a catalog with another price gives that price.
func TestPricedOnceALoad(t *testing.T) {
	writeCatalog(t, canonicalCatalog)
	ask := func() (Price, bool, Price, bool) {
		a, aok := PricedBy([]string{"anthropic", "deepseek"}, "deepseek-v4.1-flash")
		b, bok := MakerPriced([]string{"deepseek"}, "deepseek-v4.1-flash")
		return a, aok, b, bok
	}
	a, aok, b, bok := ask()
	if !aok || a.Input != 0.15 || !bok || b.Input != 0.15 {
		t.Fatalf("first look-up: %+v %v, %+v %v", a, aok, b, bok)
	}
	if _, ok := PricedBy([]string{"deepseek"}, "no-such-model"); ok {
		t.Fatal("an unlisted model priced")
	}
	runs := pricedRuns.Load()
	for range 5 {
		a2, aok2, b2, bok2 := ask()
		if a2.Input != a.Input || aok2 != aok || b2.Input != b.Input || bok2 != bok {
			t.Fatalf("the same look-up gave %+v %v, %+v %v", a2, aok2, b2, bok2)
		}
		if _, ok := PricedBy([]string{"deepseek"}, "no-such-model"); ok {
			t.Fatal("an unlisted model priced the second time")
		}
	}
	if n := pricedRuns.Load() - runs; n != 0 {
		t.Fatalf("the same models were looked for %d times again at one load", n)
	}
	// the same id from other providers is its own look-up
	if p, ok := PricedBy([]string{"alibaba-cn"}, "deepseek-v4.1-flash"); !ok || p.Input != 0.29754 {
		t.Fatalf("alibaba-cn's own price: %+v %v", p, ok)
	}
	writeCatalog(t, `{"deepseek":{"id":"deepseek","models":{"deepseek-flash":{"id":"deepseek-flash","cost":{"input":0.2,"output":0.8},"canonical_model_id":"deepseek/deepseek-v4.1-flash"}}}}`)
	if a, aok, b, bok := ask(); !aok || a.Input != 0.2 || !bok || b.Input != 0.2 {
		t.Fatalf("after a new catalog: %+v %v, %+v %v, want 0.2", a, aok, b, bok)
	}
}
