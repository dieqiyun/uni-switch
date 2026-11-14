package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// uni-switch model disable, disabled and enable (#1867): a glob and an id are
// saved as written, each said with the models it turns off now, listed,
// and taken off again; one not on the list is said so.
func TestModelDisableCmd(t *testing.T) {
	groupsHome(t)
	catalog.Changed = nil // no agent's files are rewritten here
	if _, err := refusedArgs(t, "disable"); err == nil {
		t.Fatal("no model named")
	}
	out := saidArgs(t, "disable", "*opus*", "b/gpt-5.5")
	if !strings.Contains(out, "*opus*") || !strings.Contains(out, "a/claude-opus-5-5") || !strings.Contains(out, "b/gpt-5.5") || !strings.Contains(out, "only requests through uni-switch") {
		t.Fatal(out)
	}
	if got := provider.DisabledModels(); !slices.Equal(got, []string{"*opus*", "b/gpt-5.5"}) {
		t.Fatal(got)
	}
	if out := saidArgs(t, "disabled"); !strings.Contains(out, "*opus*") || !strings.Contains(out, "a/claude-opus-5-5") {
		t.Fatal(out)
	}
	out = saidArgs(t, "enable", "*OPUS*", "nope")
	if !strings.Contains(out, "enabled again") || !strings.Contains(out, "nope was not on the disabled list") {
		t.Fatal(out)
	}
	if got := provider.DisabledModels(); !slices.Equal(got, []string{"b/gpt-5.5"}) {
		t.Fatal(got)
	}
	saidArgs(t, "list-disabled")
	saidArgs(t, "enable", "b/gpt-5.5")
	if out := saidArgs(t, "disabled"); !strings.Contains(out, "no model is disabled") {
		t.Fatal(out)
	}
}
