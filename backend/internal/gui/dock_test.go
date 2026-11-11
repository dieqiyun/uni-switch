package gui

import "testing"

// Joining the Dock to the front is activated by way of the Dock, so uni-switch
// leads Command-Tab's order (#1381), whether NSApp says it is active yet or
// not (#1651); anything else is as before.
func TestDockStep(t *testing.T) {
	cases := []struct {
		regular, on, front bool
		want               dockMove
	}{
		// already where it should be: nothing
		{true, true, true, dockMove{}},
		{false, false, true, dockMove{}},
		// the window opens (the panel, the tray's Open, a second uni-switch):
		// joins, activated by way of the Dock
		{false, true, true, dockMove{policy: true, again: true, front: true}},
		// joins for another reason, not to the front: not activated
		{false, true, false, dockMove{policy: true}},
		// the window closed: leaves, stays where it is
		{true, false, false, dockMove{policy: true}},
		// Settings takes it out with the window open: brought back to the front
		{true, false, true, dockMove{policy: true, front: true}},
	}
	for _, c := range cases {
		if got := dockStep(c.regular, c.on, c.front); got != c.want {
			t.Errorf("dockStep(regular=%v, on=%v, front=%v) = %+v, want %+v", c.regular, c.on, c.front, got, c.want)
		}
	}
}
