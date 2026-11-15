package cli

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// additional-tools=on has a provider's Responses API sent a tool Claude
// Code's tool search loads where it was loaded (#2660); default takes it
// back, and another value is refused.
func TestAdditionalToolsPair(t *testing.T) {
	var p provider.Provider
	if err := applyPairs(&p, []string{"additional-tools=on"}); err != nil || !p.AdditionalTools {
		t.Fatalf("on: %v %v", err, p.AdditionalTools)
	}
	// off turns uni-switch's own default off too (a ChatGPT account's, #4738)
	if err := applyPairs(&p, []string{"additional-tools=off"}); err != nil || p.AdditionalTools || !p.AdditionalToolsOff {
		t.Fatalf("off: %v %v %v", err, p.AdditionalTools, p.AdditionalToolsOff)
	}
	if err := applyPairs(&p, []string{"additional-tools=on"}); err != nil || !p.AdditionalTools || p.AdditionalToolsOff {
		t.Fatalf("on after off: %v %v %v", err, p.AdditionalTools, p.AdditionalToolsOff)
	}
	if err := applyPairs(&p, []string{"additional-tools=default"}); err != nil || p.AdditionalTools || p.AdditionalToolsOff {
		t.Fatalf("default: %v %v %v", err, p.AdditionalTools, p.AdditionalToolsOff)
	}
	if err := applyPairs(&p, []string{"additional-tools=maybe"}); err == nil {
		t.Fatal("maybe taken")
	}
}
