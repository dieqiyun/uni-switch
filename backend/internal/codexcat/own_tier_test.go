package codexcat

import (
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A model of a provider the user added by its address is offered the tiers
// Codex's own entry gives the same model, Ultrafast among them, and Fast on
// another GPT model; none on any other, nor on one not added by its address
// (hsiangron on X: Fast and Ultrafast weren't there to pick).
func TestOwnProviderTiers(t *testing.T) {
	v1Home(t, `{"etag":"W/\"a\"","models":[{"slug":"gpt-6-sol","base_instructions":"x","service_tiers":[{"id":"priority","name":"Fast","description":"f"},{"id":"ultrafast","name":"Ultrafast","description":"u"}]}]}`)
	ms := []catalog.Model{
		{ID: "relay/gpt-6-sol", Name: "a", OwnTier: true},
		{ID: "relay/gpt-6-sol-2026-09-01", Name: "b", OwnTier: true},
		{ID: "relay/gpt-5.5", Name: "c", OwnTier: true},
		{ID: "relay/glm-5", Name: "d", OwnTier: true},
		{ID: "openrouter/gpt-6-sol", Name: "e"},
	}
	var got struct {
		Models []struct {
			Slug  string `json:"slug"`
			Tiers []struct {
				ID string `json:"id"`
			} `json:"service_tiers"`
		} `json:"models"`
	}
	if err := json.Unmarshal(Catalog(ms), &got); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, m := range got.Models {
		s := ""
		for _, x := range m.Tiers {
			s += x.ID + " "
		}
		ids[m.Slug] = s
	}
	want := map[string]string{"relay/gpt-6-sol": "priority ultrafast ", "relay/gpt-6-sol-2026-09-01": "priority ultrafast ",
		"relay/gpt-5.5": "priority ", "relay/glm-5": "", "openrouter/gpt-6-sol": ""}
	for k, v := range want {
		if ids[k] != v {
			t.Errorf("%s offered %q, want %q", k, ids[k], v)
		}
	}
}

// What this uni-switch tells another that has it as its provider: the tiers
// its Codex is offered on each model — a ChatGPT account's own model with
// those of Codex's own entry, Ultrafast among them, a relay's GPT model
// Fast, none on any other — and that one's Codex is offered the same on
// the model, with Codex's own names (#1234).
func TestServiceTiersGoOnToAnotherUniSwitch(t *testing.T) {
	v1Home(t, `{"etag":"W/\"a\"","models":[{"slug":"gpt-6.1-sol","base_instructions":"x","service_tiers":[{"id":"priority","name":"Fast","description":"f"},{"id":"ultrafast","name":"Ultrafast","description":"u"}]}]}`)
	here := ServiceTiers([]catalog.Model{
		{ID: "codex/gpt-6.1-sol", Name: "a"},
		{ID: "relay/gpt-5.5", Name: "b", OwnTier: true},
		{ID: "relay/glm-5", Name: "c", OwnTier: true},
	})
	ids := func(ts []tier) string {
		s := ""
		for _, x := range ts {
			s += x.ID + ":" + x.Name + " "
		}
		return s
	}
	want := map[string]string{"codex/gpt-6.1-sol": "priority:Fast ultrafast:Ultrafast ", "relay/gpt-5.5": "priority:Fast ", "relay/glm-5": ""}
	for k, v := range want {
		if got := ids(here[k]); got != v {
			t.Errorf("%s tells %q, want %q", k, got, v)
		}
	}
	var there []catalog.Model
	for _, id := range []string{"codex/gpt-6.1-sol", "relay/glm-5"} {
		var list []string
		for _, x := range here[id] {
			list = append(list, x.ID)
		}
		there = append(there, catalog.Model{ID: "office/" + id, Name: id, Tiers: list})
	}
	got := ServiceTiers(there)
	if s := ids(got["office/codex/gpt-6.1-sol"]); s != "priority:Fast ultrafast:Ultrafast " {
		t.Errorf("the other uni-switch's Codex offered %q", s)
	}
	if s := ids(got["office/relay/glm-5"]); s != "" {
		t.Errorf("a model offered no tier there is offered %q", s)
	}
}
