package update

import (
	"strings"
	"testing"
)

// A Mac restart by itself with no window up opens the app as a login launch
// does, its menu bar icon only (#2096); one with the window opens it. Both
// ask for a new instance (-n), so LaunchServices launches the bundle rather
// than send a reopen to the uni-switch that has just exited (Linka's -600).
//
// Only a restart the user asked for, with its window coming back, brings
// uni-switch to the front. One uni-switch made by itself (quiet), or one with only
// the menu bar icon to come back, opens in the background (-g): without
// it, Settings' AutoRestart took the focus from the app the user was
// typing in (xyxyxy_15591 on Discord).
func TestRelaunchOpen(t *testing.T) {
	const app = "/Applications/uni-switch.app"
	for _, c := range []struct {
		window, quiet bool
		want          string
	}{
		{true, false, `open -n "/Applications/uni-switch.app"`},
		{true, true, `open -n -g "/Applications/uni-switch.app"`},
		{false, false, `open -n -g "/Applications/uni-switch.app" --args tray`},
		{false, true, `open -n -g "/Applications/uni-switch.app" --args tray`},
	} {
		if got := relaunchOpen(app, c.window, c.quiet); got != c.want {
			t.Errorf("relaunch, window %v quiet %v: %s, want %s", c.window, c.quiet, got, c.want)
		}
		// a restart that hands the gateway over opens the new version
		// while this one runs: a new instance (-n), which LaunchServices
		// would otherwise send to the one running, as quiet as Relaunch's
		if got := openBeside(app, c.window, c.quiet); got != c.want {
			t.Errorf("beside, window %v quiet %v: %s, want %s", c.window, c.quiet, got, c.want)
		}
	}
}

// The new version a handover starts isn't told to wait for this one to
// exit (it would wait while this one waits for it), and gets the args
// RelaunchBinary gives.
func TestStartBesideWaitsForNothing(t *testing.T) {
	t.Setenv("UNI_SWITCH_REPLACES", "")
	var gotArgs, gotEnv []string
	stubLaunch(t, func(exe string, args, env []string) (<-chan struct{}, func(), error) {
		gotArgs, gotEnv = args, env
		return nil, nil, nil
	})
	if err := StartBeside("/opt/uni-switch", true, "routing"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotArgs, " ") != "gui routing" {
		t.Errorf("args %q", gotArgs)
	}
	for _, e := range gotEnv {
		if strings.HasPrefix(e, "UNI_SWITCH_REPLACES=") && e != "UNI_SWITCH_REPLACES=" {
			t.Errorf("told to wait: %s", e)
		}
	}
}
