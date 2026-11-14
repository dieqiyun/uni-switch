package gui

import "testing"

// TestCopyHeld: a copy counts as made only when the clipboard was read
// back holding the text itself (#1881). GTK 3 under KDE Plasma without a
// primary selection answered every paste with an empty pipe while uni-switch
// said "copied".
func TestCopyHeld(t *testing.T) {
	const text = "claude --resume 1881"
	for _, c := range []struct {
		name string
		read int
		held string
		want bool
	}{
		{"read back whole", 1, text, true},
		{"read back empty (#1881)", 1, "", false},
		{"read back another copy", 1, "something else", false},
		{"read back cut short", 1, text[:6], false},
		{"holds no text", 0, "", false},
		{"can't tell", -1, "", false},
	} {
		if got := copyHeld(c.read, c.held, text); got != c.want {
			t.Errorf("%s: copyHeld(%d, %q) = %v, want %v", c.name, c.read, c.held, got, c.want)
		}
	}
}
