package awake

import (
	"slices"
	"testing"
)

// TestCaffeinateArgs keeps the display awake with -d only when asked (#975).
func TestCaffeinateArgs(t *testing.T) {
	if got, want := caffeinateArgs(false, 42), []string{"-i", "-w", "42"}; !slices.Equal(got, want) {
		t.Errorf("system: %q, want %q", got, want)
	}
	if got, want := caffeinateArgs(true, 42), []string{"-i", "-d", "-w", "42"}; !slices.Equal(got, want) {
		t.Errorf("display: %q, want %q", got, want)
	}
}
