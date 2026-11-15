package provider

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// Kiro's plugin before 0.1.24 answered a too-long conversation that Kiro
// said in its event stream with a 502, and a reply Kiro stopped for a
// full window as a finished one, so uni-switch never read either as an
// overflow (TestKiroPluginOverflowGoesToAMemberWithRoom in the gateway).
// The mover keeps every moved user on a version that says it.
func TestKiroMoverHasTheTooLongFix(t *testing.T) {
	if update.Newer("0.1.24", movers["kiro"].min) {
		t.Fatalf("the Kiro mover installs opencode-kiro-auth %s, which answers Kiro's too-long stream with a 502", movers["kiro"].min)
	}
}
