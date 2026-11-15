package cli

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// PAMI on Discord: uni-switch model price m 10,50,1,12.5 --tier 272k 20,75,2,25
// gives what the whole request costs over 272K input, as OpenAI bills
// gpt-6-astra, and a fifth part is the 1-hour cache write. Both are kept,
// counted and shown; a size that isn't one, or a tier short of a part, is
// refused.
func TestModelPriceTierCmd(t *testing.T) {
	groupsHome(t)
	if err := modelCmd([]string{"price", "a/m", "10,50,1,12.5", "--tier", "272k", "20,75,2,25", "--tier=1M", "40", "100", "4", "50"}); err != nil {
		t.Fatal(err)
	}
	pr, ok := provider.EffectivePrice("a", "m")
	want := catalog.Price{Input: 10, Output: 50, CacheRead: 1, CacheWrite: 12.5, Tiers: []catalog.Tier{
		{Above: 272000, Input: 20, Output: 75, CacheRead: 2, CacheWrite: 25},
		{Above: 1000000, Input: 40, Output: 100, CacheRead: 4, CacheWrite: 50}}}
	if !ok || !pr.Same(want) {
		t.Fatalf("price = %+v, want %+v", pr, want)
	}
	out, err := said(t, func() error { return modelCmd([]string{"price", "a/m"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "over 272K input tokens") || !strings.Contains(out, "over 1M input tokens") || !strings.Contains(out, "$20/$75") {
		t.Errorf("uni-switch model price a/m: %q; want its tiers", out)
	}
	out, _ = said(t, func() error { return modelCmd([]string{"prices"}) })
	if !strings.Contains(out, "over 272K") {
		t.Errorf("uni-switch model prices: %q; want its tier", out)
	}

	// PAMI on Discord: four parts give no 1-hour cache write but a Claude
	// model's — gpt-6-astra was shown "$20 (1 hour) at 2× input", a write
	// OpenAI has no price for
	if strings.Contains(out, "1 hour") || strings.Contains(out, "2× input") {
		t.Errorf("uni-switch model price a/m: %q; want no 1-hour cache write", out)
	}
	if pr.OneHour() != 12.5 {
		t.Errorf("a/m 1h = %v, want the 5-minute 12.5", pr.OneHour())
	}
	if err := modelCmd([]string{"price", "a/claude-opus-5-5", "3,15,0.3,3.75"}); err != nil {
		t.Fatal(err)
	}
	if pr, _ := provider.EffectivePrice("a", "claude-opus-5-5"); pr.OneHour() != 6 {
		t.Errorf("claude 1h = %+v, want 2× input", pr)
	}
	out, _ = said(t, func() error { return modelCmd([]string{"price", "a/claude-opus-5-5"}) })
	if !strings.Contains(out, "$6.00 (1 hour)") && !strings.Contains(out, "$6 (1 hour)") {
		t.Errorf("uni-switch model price a/claude-opus-5-5: %q; want its 1-hour cache write", out)
	}

	if err := modelCmd([]string{"price", "a/only-a", "5,25,0.5,6.25,8"}); err != nil {
		t.Fatal(err)
	}
	if pr, _ := provider.EffectivePrice("a", "only-a"); pr.CacheWrite1h != 8 || pr.OneHour() != 8 {
		t.Fatalf("1h = %+v", pr)
	}
	out, _ = said(t, func() error { return modelCmd([]string{"price", "a/only-a"}) })
	if !strings.Contains(out, "(1 hour)") {
		t.Errorf("uni-switch model price a/only-a: %q; want its 1-hour cache write", out)
	}

	for _, args := range [][]string{
		{"10,50,1,12.5", "--tier", "lots", "20,75,2,25"},
		{"10,50,1,12.5", "--tier", "272k", "20,75"},
		{"10,50,1,12.5", "--tier"},
		{"10,50,1,12.5", "--tier", "272k", "20,75,2,25", "--tier", "272k", "20,75,2,25"},
	} {
		if err := modelCmd(append([]string{"price", "a/m"}, args...)); err == nil {
			t.Errorf("%q was taken", args)
		}
	}
	if pr, _ := provider.EffectivePrice("a", "m"); !pr.Same(want) {
		t.Errorf("a refused price was written: %+v", pr)
	}
}
