package provider

import (
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// OpenCode Zen serves TypeSafe's Jev on System One alone, at
// /zen/v1/systemone (its docs' model table, zen.mdx): Jev routes groups
// there, as on TypeSafe's own API, and is never an agent's chat model —
// a Zen provider saved before the preset said so included.
func TestOpenCodeZenJevDecides(t *testing.T) {
	detectHome(t)
	pr, err := FromPreset("opencode-zen")
	if err != nil {
		t.Fatal(err)
	}
	if pr.Decide != "https://opencode.ai/zen/v1" {
		t.Fatalf("preset decide %q", pr.Decide)
	}
	old := pr
	old.Decide = "" // saved before the preset had it
	if err := Save(old); err != nil {
		t.Fatal(err)
	}
	found, err := Find(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := *found
	if p.Decide != pr.Decide || p.DecideOnly() {
		t.Fatalf("saved Zen decide %q", p.Decide)
	}
	live := []catalog.Model{{ID: "deepseek-v4.1-flash"}, {ID: "jev-1.13"}, {ID: "jev-1.13-free"}, {ID: "mimo-v2.6-flash-free"}}
	if err := catalog.SaveLive(p.ID, p.Chat, live); err != nil {
		t.Fatal(err)
	}
	if !p.DecidesModel("jev-1.13-free") || p.DecidesModel("deepseek-v4.1-flash") {
		t.Fatal("Jev isn't Zen's decision model")
	}
	var ids []string
	for _, e := range providerEntries() {
		if e.Provider.ID == p.ID {
			ids = append(ids, e.Model)
		}
	}
	if slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, "jev") }) || !slices.Contains(ids, "deepseek-v4.1-flash") {
		t.Fatalf("agents' models %v", ids)
	}
	// asked with no key of the user's, the free Jev; with one, Zen's first
	if j := p.Jev(); j != "jev-1.13-free" {
		t.Fatalf("keyless Jev %q", j)
	}
	if u, _ := p.DecideURL(t.Context()); u != "https://opencode.ai/zen/v1/systemone" {
		t.Fatalf("asked at %q", u)
	}
	paid := p
	paid.Key = "sk-own"
	if j := paid.Jev(); j != "jev-1.13" {
		t.Fatalf("keyed Jev %q", j)
	}
}

// A preset's model (OpenCode Go's deepseek-v4.1-flash, which answers on
// Responses as well as chat) and a subscription's can be asked on one API
// the user picks, as a custom provider's can (01huadalang on Discord); Auto
// is what the vendor's list says.
func TestPresetAndSubscriptionModelAPI(t *testing.T) {
	detectHome(t)
	p, err := FromPreset("opencode-go")
	if err != nil {
		t.Fatal(err)
	}
	p.Key, p.Models = "k", []string{"deepseek-v4.1-flash", "minimax-m3"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := SetModelAPI("opencode-go/deepseek-v4.1-flash", "responses"); err != nil {
		t.Fatal(err)
	}
	got, _ := Find("opencode-go")
	p = *got
	if apis := p.APIs("deepseek-v4.1-flash"); !slices.Equal(apis, []Protocol{Responses}) {
		t.Fatalf("apis %v", apis)
	}
	if n := p.Native("deepseek-v4.1-flash"); n != Responses {
		t.Fatalf("native %s", n)
	}
	if apis := p.ListedAPIs("deepseek-v4.1-flash"); slices.Equal(apis, []Protocol{Responses}) {
		t.Fatalf("the user's pick taken for the vendor's: %v", apis)
	}
	// a subscription with more than one API: the pick is honoured too
	sub := Provider{ID: "copilot", Name: "Copilot", Account: &Account{Agent: "copilot", User: "u"},
		Chat: "https://api.example.com", Responses: "https://api.example.com", Anthropic: "https://api.example.com"}
	s := settings.Load()
	s.ModelAPIs = map[string]string{"copilot/gpt-9": "chat"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if got, ok := sub.ModelAPI("gpt-9"); !ok || got != Chat {
		t.Fatalf("subscription ModelAPI %v %v", got, ok)
	}
	if apis := sub.APIs("gpt-9"); !slices.Equal(apis, []Protocol{Chat}) {
		t.Fatalf("subscription apis %v", apis)
	}
	// one with a single API has nothing to pick
	one := Provider{ID: "codex", Account: &Account{Agent: "codex"}, Responses: "https://chatgpt.example.com"}
	s.ModelAPIs = map[string]string{"codex/gpt-9": "chat"}
	settings.Save(s)
	if _, ok := one.ModelAPI("gpt-9"); ok {
		t.Fatal("a single-API subscription took a pick")
	}
}
