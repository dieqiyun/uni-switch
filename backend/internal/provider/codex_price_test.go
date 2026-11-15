package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestAutoReviewUsesSharedCatalogPrice(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0700); err != nil {
		t.Fatal(err)
	}
	// Deliberately different prices: the alias must use the catalog, never
	// carry its own rates. Preserve exact-name prices if the catalog adds one.
	write := func(models string) {
		t.Helper()
		if err := os.WriteFile(catalog.CachePath(), []byte(`{"openai":{"id":"openai","models":{`+models+`}}}`), 0600); err != nil {
			t.Fatal(err)
		}
		catalog.Reset()
	}
	t.Cleanup(catalog.Reset)
	write(`"gpt-5.6-luna":{"id":"gpt-5.6-luna","cost":{"input":3,"output":7,"cache_read":1}}`)
	pr, ok := MakerPrice("codex-auto-review")
	if !ok || pr.Input != 3 || pr.Output != 7 || pr.CacheRead != 1 {
		t.Fatalf("alias did not use shared catalog: %+v, %v", pr, ok)
	}
	if PricedName("codex-auto-review") != "gpt-5.6-luna" {
		t.Fatal("missing price reference")
	}
	write(`"codex-auto-review":{"id":"codex-auto-review","cost":{"input":4,"output":8}}`)
	if pr, ok := MakerPrice("codex-auto-review"); !ok || pr.Input != 4 {
		t.Fatalf("exact-name catalog price lost: %+v, %v", pr, ok)
	}
	if PricedName("codex-auto-review") != "codex-auto-review" {
		t.Fatal("price reference must agree with exact-name catalog price")
	}
	write(`"unrelated":{"id":"unrelated","cost":{"input":1,"output":2}}`)
	if _, ok := MakerPrice("codex-auto-review"); ok {
		t.Fatal("missing catalog price must remain unknown")
	}
}

// OpenAI made Auto-review free for every user signed in through a ChatGPT
// account (silverzhou on Discord, thsottiaux's "Day 2.1"): Codex's
// codex-auto-review through the Codex subscription costs nothing in the
// estimates, but not on a Free plan, where it spends the allowance
// (cybercaius), nor through anything that isn't a ChatGPT sign-in.
func TestAutoReviewFreeOnChatGPTSignIn(t *testing.T) {
	home := signIn(t) // a Pro plan
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"openai":{"id":"openai","models":{`+
		`"gpt-5.6-luna":{"id":"gpt-5.6-luna","cost":{"input":3,"output":7,"cache_read":1}}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	s := settings.Load()
	price := func(prov, model string) catalog.Price {
		t.Helper()
		pr, ok := EffectivePriceIn(s, prov, model)
		if !ok {
			t.Fatalf("%s/%s: no price", prov, model)
		}
		return pr
	}
	if pr := price("codex", "codex-auto-review"); pr.Cost(1e6, 1e6, 1e6, 0) != 0 {
		t.Errorf("auto-review on a Pro sign-in priced %+v", pr)
	}
	if pr := price("codex", " Codex-Auto-Review "); pr.Cost(1e6, 1e6, 0, 0) != 0 {
		t.Errorf("auto-review spelled otherwise priced %+v", pr)
	}
	if got := PricedNameFor("codex", "codex-auto-review"); got != "codex-auto-review" {
		t.Errorf("a free call said to be priced as %q", got)
	}
	// a review that followed the session, or ran on Codex's other models
	if pr := price("codex", "gpt-5.6-luna"); pr.Input != 3 {
		t.Errorf("codex gpt-5.6-luna %+v", pr)
	}
	// the same model through anything else is OpenAI's list price
	for _, prov := range []string{"openai", ChatGPTAPIID, "relay"} {
		if pr := price(prov, "codex-auto-review"); pr.Input != 3 || pr.Output != 7 {
			t.Errorf("%s auto-review %+v", prov, pr)
		}
	}
	// what the user set wins
	two, four, zero := 2.0, 4.0, 0.0
	s.ModelPrices = map[string]settings.ModelPrice{"codex/codex-auto-review": {Input: &two, Output: &four, CacheRead: &zero, CacheWrite: &zero}}
	if pr := price("codex", "codex-auto-review"); pr.Input != 2 {
		t.Errorf("the user's price lost: %+v", pr)
	}
	s.ModelPrices = nil

	// a Free plan spends its allowance on it: priced as gpt-5.6-luna
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token":      fakeJWT(map[string]any{"email": "me@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "free", "chatgpt_account_id": "acct-1"}}),
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r", "account_id": "acct-1",
		},
	})
	forgetAccountCaches()
	if pr := price("codex", "codex-auto-review"); pr.Input != 3 || pr.Output != 7 {
		t.Errorf("auto-review on a Free plan priced %+v", pr)
	}
	if got := PricedNameFor("codex", "codex-auto-review"); got != "gpt-5.6-luna" {
		t.Errorf("Free plan's review priced as %q", got)
	}

	// signed out since: the subscription's calls were a ChatGPT sign-in's
	os.Remove(filepath.Join(home, ".codex", "auth.json"))
	forgetAccountCaches()
	if pr := price("codex", "codex-auto-review"); pr.Cost(1e6, 1e6, 0, 0) != 0 {
		t.Errorf("auto-review of a signed-out Codex priced %+v", pr)
	}
}
