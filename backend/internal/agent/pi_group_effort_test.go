package agent

import (
	"testing"
)

// Pi's clamp: the level itself, else the nearest above, else below.
func TestPiClamp(t *testing.T) {
	for _, c := range []struct {
		level   string
		offered []string
		want    string
	}{
		{"max", []string{"off"}, "off"},
		{"max", []string{"low", "high", "max"}, "max"},
		{"medium", []string{"low", "high", "max"}, "high"},
		{"xhigh", []string{"off", "low", "medium", "high"}, "high"},
		{"minimal", []string{"low", "high"}, "low"},
	} {
		if got := piClamp(c.level, c.offered); got != c.want {
			t.Errorf("%s of %v: %s, want %s", c.level, c.offered, got, c.want)
		}
	}
}
