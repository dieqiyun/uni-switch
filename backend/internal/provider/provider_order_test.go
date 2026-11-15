package provider

import (
	"slices"
	"testing"
)

// #1940 (Sun1090): WorkBuddy and WorkBuddy AI were put first on one
// computer, and the order synced to another kept them where they were. A
// subscription served by its plugin is workbuddy-plugin where the built-in
// isn't moved onto it and workbuddy where it is, and that is each
// computer's own, so the order synced names it by the other computer's id.
// It keeps its place by either.
func TestOrderKeepsAPluginsPlaceByEitherID(t *testing.T) {
	ps := func(ids ...string) []Provider {
		var out []Provider
		for _, id := range ids {
			out = append(out, Provider{ID: id})
		}
		return out
	}
	ids := func(ps []Provider) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.ID)
		}
		return out
	}
	// put in order where both were moved: workbuddy-ai, workbuddy
	order := []string{WorkBuddyAIID, "workbuddy", "zhipu", "deepseek"}
	// here neither is moved, so the plugin signed in under its own id
	got := ids(ordered(ps("zhipu", "deepseek", "workbuddy-plugin", WorkBuddyAIID+"-plugin"), order))
	if want := []string{WorkBuddyAIID + "-plugin", "workbuddy-plugin", "zhipu", "deepseek"}; !slices.Equal(got, want) {
		t.Fatalf("not moved here: %v, want %v", got, want)
	}
	// and the other way round
	order = []string{"workbuddy-plugin", WorkBuddyAIID + "-plugin", "zhipu"}
	got = ids(ordered(ps("zhipu", WorkBuddyAIID, "workbuddy"), order))
	if want := []string{"workbuddy", WorkBuddyAIID, "zhipu"}; !slices.Equal(got, want) {
		t.Fatalf("moved here: %v, want %v", got, want)
	}
	// an id the order names itself wins over its counterpart's place
	got = ids(ordered(ps("zhipu", "workbuddy", "workbuddy-plugin"), []string{"workbuddy-plugin", "zhipu", "workbuddy"}))
	if want := []string{"workbuddy-plugin", "zhipu", "workbuddy"}; !slices.Equal(got, want) {
		t.Fatalf("both here: %v, want %v", got, want)
	}
	// a custom provider whose id ends in -plugin is only itself
	got = ids(ordered(ps("zhipu", "my-plugin", "my"), []string{"my", "zhipu"}))
	if want := []string{"my", "zhipu", "my-plugin"}; !slices.Equal(got, want) {
		t.Fatalf("custom: %v, want %v", got, want)
	}
}
