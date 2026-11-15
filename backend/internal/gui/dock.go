package gui

// dockMove is what setDock does to the app on the Mac.
type dockMove struct {
	policy bool // join the Dock or leave it
	again  bool // activate uni-switch again by way of the Dock (activateAgain)
	front  bool // activate uni-switch
}

// dockStep: what setDock does for uni-switch in the Dock (regular) or not, to
// be in it (on) or not, brought to the front or not. Nothing when it is
// where it should be. Leaving deactivates the app, so with front it is
// activated after. Joining to the front, it is activated by way of the Dock:
// an app activated out of the Dock that joins it stays last in Command-Tab's
// order, and one Command-Tab away couldn't come back to the window (#1381).
// That is so whether or not NSApp says it is active yet: the Mac makes it
// the front app at once, and NSApp says so only once the main thread has
// taken the activation in, which on a busy Mac comes after the join (#1651).
// Not active at all, the Dock's turn in front changes nothing.
func dockStep(regular, on, front bool) dockMove {
	if regular == on {
		return dockMove{}
	}
	return dockMove{policy: true, again: on && front, front: front}
}
