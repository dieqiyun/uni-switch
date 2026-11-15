package provider

import (
	"slices"
	"testing"
)

func TestFallbackIsKept(t *testing.T) {
	signIn(t)
	if err := Save(Provider{ID: "plan", Name: "Plan", Key: "k", Chat: "https://plan.example/v1",
		Fallback: []string{" codex/gpt-5.5 ", "", "codex/gpt-5.5", "other/m"}}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("plan")
	if err != nil || !slices.Equal(p.Fallback, []string{"codex/gpt-5.5", "other/m"}) {
		t.Fatalf("custom provider fallback: %v %v", p, err)
	}
	// a signed-in account keeps its fallback next to its model picks
	if err := Save(Provider{ID: "codex", Fallback: []string{"plan/m1"}}); err != nil {
		t.Fatal(err)
	}
	if a, err := Find("codex"); err != nil || a.Account == nil || !slices.Equal(a.Fallback, []string{"plan/m1"}) {
		t.Fatalf("account fallback: %+v %v", a, err)
	}
}
