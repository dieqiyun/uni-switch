package provider

import (
	"os"
	"strings"
	"testing"
)

// An account's weight is kept by its name in lower case, 1 left out of the
// file, a weight out of 0–MaxKeyWeight or an account the subscription
// hasn't refused, a provider with keys refused (its keys have their own);
// the accounts All gives carry it, as the gateway routes them.
func TestSetAccountWeight(t *testing.T) {
	signIn(t) // me@example.com on codex
	if err := SetAccountWeight("codex", "Me@Example.com", 4); err != nil {
		t.Fatal(err)
	}
	p, err := Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	if w := p.AccountWeight("ME@example.com"); w != 4 {
		t.Fatalf("weight %d", w)
	}
	if p.AccountWeights["me@example.com"] != 4 {
		t.Fatalf("kept as %v", p.AccountWeights)
	}
	carried := false
	for _, a := range All() {
		if a.ID == "codex" && a.Account != nil && strings.EqualFold(a.Account.User, "me@example.com") {
			carried = a.AccountWeight(a.Account.User) == 4
		}
	}
	if !carried {
		t.Fatal("the account All gives doesn't weigh 4")
	}
	for _, bad := range []int{-1, MaxKeyWeight + 1} {
		if err := SetAccountWeight("codex", "me@example.com", bad); err == nil {
			t.Fatalf("%d taken", bad)
		}
	}
	if err := SetAccountWeight("codex", "nobody@example.com", 2); err == nil {
		t.Fatal("an account it hasn't was weighed")
	}
	if err := SetAccountWeight("codex", "me@example.com", 1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	if strings.Contains(string(b), "accountWeights") {
		t.Fatalf("1 was written: %s", b)
	}
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k1", Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := SetAccountWeight("relay", "k1", 3); err == nil {
		t.Fatal("a key provider's account was weighed")
	}
	if got := normalAccountWeights(map[string]int{" A@x ": 2, "b": 1, "c": 5000}); len(got) != 2 || got["a@x"] != 2 || got["c"] != MaxKeyWeight {
		t.Fatalf("normal %v", got)
	}
}

// A Copilot account renamed on GitHub keeps its weight under its new name.
func TestAccountWeightMovesWithRename(t *testing.T) {
	signIn(t)
	if err := Save(Provider{ID: "copilot", Name: "Copilot", Account: &Account{}, AccountWeights: map[string]int{"old": 3}}); err != nil {
		t.Fatal(err)
	}
	if err := moveAccountSettings("copilot", map[string]string{"Old": "New"}); err != nil {
		t.Fatal(err)
	}
	f, err := read()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range f.Providers {
		if p.ID == "copilot" {
			found = true
			if p.AccountWeights["new"] != 3 || p.AccountWeights["old"] != 0 {
				t.Fatalf("weights after the rename %v", p.AccountWeights)
			}
		}
	}
	if !found {
		t.Fatal("copilot not saved")
	}
}
