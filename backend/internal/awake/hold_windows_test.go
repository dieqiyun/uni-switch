package awake

import "testing"

// TestExecutionState asks for the display only when asked (#975).
func TestExecutionState(t *testing.T) {
	if got := executionState(false); got != esContinuous|esSystemRequired {
		t.Errorf("system: %#x", got)
	}
	if got := executionState(true); got != esContinuous|esSystemRequired|esDisplayRequired {
		t.Errorf("display: %#x", got)
	}
}
