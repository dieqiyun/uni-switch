package catalog

import "testing"

// TestNearestEffortTakesVendorWords: a model listing Trae's light, high and
// extra_high (Our Free Model's plugin, nianshou5) is asked low as light and
// xhigh as extra_high, in its own words, and its light as it is; a model in
// EffortRank's words is fitted as before.
func TestNearestEffortTakesVendorWords(t *testing.T) {
	trae := []string{"light", "high", "extra_high"}
	for _, tc := range []struct {
		want   string
		levels []string
		got    string
	}{
		{"low", trae, "light"},
		{"light", trae, "light"},
		{"minimal", trae, "light"},
		{"medium", trae, "high"},
		{"xhigh", trae, "extra_high"},
		{"extra_high", trae, "extra_high"},
		{"max", trae, "extra_high"},
		{"light", []string{"low", "high"}, "low"},
		{"extra_high", []string{"low", "high", "max"}, "max"},
		{"low", []string{"low", "high"}, "low"},
		{"medium", []string{"low", "high", "max"}, "high"},
	} {
		if got := NearestEffort(tc.want, tc.levels); got != tc.got {
			t.Errorf("NearestEffort(%q, %v) = %q, want %q", tc.want, tc.levels, got, tc.got)
		}
	}
}
