package catalog

import "testing"

// NearestEffort: the level itself where it is taken; else the closest by
// rank, a tie going up; never "none" as a stand-in; unknown or no levels
// leave it as asked.
func TestNearestEffort(t *testing.T) {
	for _, c := range []struct {
		want   string
		levels []string
		got    string
	}{
		{"xhigh", []string{"low", "medium", "high", "xhigh", "max"}, "xhigh"},
		{"xhigh", []string{"low", "high", "max"}, "max"},
		{"xhigh", []string{"low", "medium", "high"}, "high"},
		{"max", []string{"low", "medium", "high"}, "high"},
		{"medium", []string{"low", "high"}, "high"},
		{"minimal", []string{"none", "low", "high"}, "low"},
		{"none", []string{"low", "high", "max"}, "low"},
		{"high", nil, "high"},
		{"odd", []string{"low"}, "odd"},
	} {
		if g := NearestEffort(c.want, c.levels); g != c.got {
			t.Errorf("NearestEffort(%q, %v) = %q, want %q", c.want, c.levels, g, c.got)
		}
	}
}
