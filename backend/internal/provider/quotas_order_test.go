package provider

import (
	"slices"
	"testing"
)

// Zhenbao TONG on Discord: the providers moved on the window's Providers
// page, and the tray panel's Allowances tab kept its own order. The cards
// come in the providers' order now, each provider's as they came, and a
// card of no provider here (a remote uni-switch's) after them.
func TestQuotaCardsInProviderOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, id := range []string{"a", "b", "c"} {
		if err := Save(Provider{ID: id, Name: id, Key: "sk-" + id, Chat: "https://" + id + ".example.com/v1", Models: []string{"m"}}); err != nil {
			t.Fatal(err)
		}
	}
	// as Quotas has them: subscriptions, then plans, then balances
	qs := []SubscriptionQuota{
		{Provider: "a", User: "a1"}, {Provider: "remote:x", User: "r"}, {Provider: "c", User: "c1"},
		{Provider: "b", Plan: "pro"}, {Provider: "a", User: "a2"}, {Provider: "c", Balance: "$1"},
	}
	cards := func(qs []SubscriptionQuota) []string {
		var out []string
		for _, q := range qs {
			out = append(out, q.Provider+"/"+q.User+q.Plan+q.Balance)
		}
		return out
	}
	if got, want := cards(InProviderOrder(qs)), []string{"a/a1", "a/a2", "b/pro", "c/c1", "c/$1", "remote:x/r"}; !slices.Equal(got, want) {
		t.Fatalf("as added: %v, want %v", got, want)
	}
	if err := SetOrder([]string{"c", "a", "b"}); err != nil {
		t.Fatal(err)
	}
	if got, want := cards(InProviderOrder(qs)), []string{"c/c1", "c/$1", "a/a1", "a/a2", "b/pro", "remote:x/r"}; !slices.Equal(got, want) {
		t.Fatalf("after SetOrder(c, a, b): %v, want %v", got, want)
	}
	// the cache's own array is left as it was
	if qs[0].User != "a1" || qs[1].User != "r" {
		t.Fatalf("Quotas' list was sorted in place: %v", cards(qs))
	}
}
