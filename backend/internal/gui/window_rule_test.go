package gui

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The window opens at the size it was last given, the default before
// that, and never smaller than its least.
func TestOpenSize(t *testing.T) {
	for _, c := range []struct {
		name       string
		saved      []int
		minW, minH int
		w, h       int
	}{
		{"default", nil, 560, 420, windowW, windowH},
		{"kept", []int{1713, 1084}, 560, 420, 1713, 1084},
		{"under the least", []int{300, 200}, 560, 420, windowW, windowH},
		{"malformed", []int{900}, 560, 420, windowW, windowH},
		{"least at a larger text size", []int{600, 450}, 840, 630, 840, 630},
	} {
		if w, h := openSize(c.saved, c.minW, c.minH); w != c.w || h != c.h {
			t.Errorf("%s: openSize = %d×%d, want %d×%d", c.name, w, h, c.w, c.h)
		}
	}
}

// A window larger than its screen's work area is fitted to it, so its
// title bar is in sight; one that fits, or on a screen not known, is left.
func TestFitRoom(t *testing.T) {
	for _, c := range []struct {
		name         string
		w, h, sw, sh int
		fw, fh       int
		changed      bool
	}{
		{"fits", 1200, 800, 1920, 1040, 1200, 800, false},
		{"kept on a larger screen", 2400, 1400, 1920, 1040, 1920, 1040, true},
		{"too tall only", 1200, 1300, 1920, 1040, 1200, 1040, true},
		{"screen unknown", 2400, 1400, 0, 0, 2400, 1400, false},
	} {
		if fw, fh, changed := fitRoom(c.w, c.h, c.sw, c.sh); fw != c.fw || fh != c.fh || changed != c.changed {
			t.Errorf("%s: fitRoom = %d×%d %v, want %d×%d %v", c.name, fw, fh, changed, c.fw, c.fh, c.changed)
		}
	}
}

// The window is kept as it settles: maximised or not, and the size it is
// restored to. Maximised, the size kept stays the one the user gave it, so
// it opens maximised and restores to that size (on a MacBook: zoomed last,
// it opened at an older size a few points short of the screen). Fitted to a
// smaller screen, it keeps the larger size for the larger screen.
func TestSettle(t *testing.T) {
	type step struct {
		w, h, sw, sh int
		maximised    bool
		changed      bool
		window       []int
		max          bool
	}
	for _, c := range []struct {
		name  string
		steps []step
	}{
		{"resized", []step{{900, 700, 0, 0, false, true, []int{900, 700}, false}}},
		{"maximised keeps its size", []step{
			{900, 700, 0, 0, false, true, []int{900, 700}, false},
			{1728, 1084, 0, 0, true, true, []int{900, 700}, true},
		}},
		{"restored", []step{
			{900, 700, 0, 0, false, true, []int{900, 700}, false},
			{1728, 1084, 0, 0, true, true, []int{900, 700}, true},
			{899, 699, 0, 0, false, true, []int{900, 700}, false},
		}},
		{"maximised again, nothing new", []step{
			{1728, 1084, 0, 0, true, true, nil, true},
			{1728, 1084, 0, 0, true, false, nil, true},
		}},
		{"a point short", []step{
			{900, 700, 0, 0, false, true, []int{900, 700}, false},
			{899, 699, 0, 0, false, false, []int{900, 700}, false},
		}},
		{"under the least", []step{{300, 200, 0, 0, false, false, nil, false}}},
		{"fitted to a smaller screen", []step{
			{2400, 1400, 0, 0, false, true, []int{2400, 1400}, false},
			{1920, 1040, 1920, 1040, false, false, []int{2400, 1400}, false},
		}},
		{"fitted, then made narrower", []step{
			{2400, 1400, 0, 0, false, true, []int{2400, 1400}, false},
			{1500, 1040, 1920, 1040, false, true, []int{1500, 1400}, false},
		}},
		{"as large as the screen, nothing larger kept", []step{
			{1920, 1040, 1920, 1040, false, true, []int{1920, 1040}, false},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var s settings.Settings
			for i, st := range c.steps {
				changed := settle(&s, st.w, st.h, st.sw, st.sh, st.maximised)
				if changed != st.changed || !slices.Equal(s.Window, st.window) || s.WindowMaximised != st.max {
					t.Fatalf("step %d: changed %v, window %v, maximised %v; want %v, %v, %v",
						i, changed, s.Window, s.WindowMaximised, st.changed, st.window, st.max)
				}
			}
		})
	}
}

// The window opens where it was left (jorbenzhu on Discord: back in the
// middle after every update), moved whole onto a screen there now; with
// no place kept, or its screen gone, it is left to the system's centre.
func TestOpenAt(t *testing.T) {
	laptop := area{0, 25, 1512, 919}      // a Mac's work area under the menu bar
	left := area{-1920, -200, 1920, 1040} // a monitor to its left, higher
	right := area{1512, 0, 2560, 1400}    // one to its right
	for _, c := range []struct {
		name  string
		saved []int
		w, h  int
		areas []area
		x, y  int
		ok    bool
	}{
		{"nothing kept", nil, 800, 600, []area{laptop}, 0, 0, false},
		{"malformed", []int{40}, 800, 600, []area{laptop}, 0, 0, false},
		{"no screen known", []int{100, 100}, 800, 600, nil, 0, 0, false},
		{"where it was", []int{300, 140}, 800, 600, []area{laptop}, 300, 140, true},
		{"on the left monitor", []int{-1500, -100}, 800, 600, []area{laptop, left}, -1500, -100, true},
		{"its monitor gone", []int{-1500, -100}, 800, 600, []area{laptop}, 0, 0, false},
		{"its right monitor gone", []int{2400, 300}, 800, 600, []area{laptop}, 0, 0, false},
		{"over the edge, onto the screen with most of it", []int{1300, 200}, 800, 600, []area{laptop, right}, 1512, 200, true},
		{"hanging off the bottom", []int{900, 700}, 800, 600, []area{laptop}, 712, 344, true},
		{"under the menu bar", []int{100, 0}, 800, 600, []area{laptop}, 100, 25, true},
		{"larger than the screen", []int{-50, 10}, 1600, 1000, []area{laptop}, 0, 25, true},
		{"no size", []int{100, 100}, 0, 0, []area{laptop}, 0, 0, false},
	} {
		x, y, ok := openAt(c.saved, c.w, c.h, c.areas)
		if x != c.x || y != c.y || ok != c.ok {
			t.Errorf("%s: openAt = %d, %d, %v; want %d, %d, %v", c.name, x, y, ok, c.x, c.y, c.ok)
		}
	}
}

// Where the window was moved to is kept, and a move to the same place
// writes nothing.
func TestSettleAt(t *testing.T) {
	var s settings.Settings
	if !settleAt(&s, -1500, -100) || !slices.Equal(s.WindowAt, []int{-1500, -100}) {
		t.Fatalf("not kept: %v", s.WindowAt)
	}
	if settleAt(&s, -1500, -100) {
		t.Fatal("the same place counted as a change")
	}
	if !settleAt(&s, 0, 25) || !slices.Equal(s.WindowAt, []int{0, 25}) {
		t.Fatalf("a new place not kept: %v", s.WindowAt)
	}
}

// A Wayland session can't place a window, so no place is kept or put
// back there; X11, XWayland by GDK_BACKEND, the Mac and Windows can.
func TestKeepsPlace(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	for _, c := range []struct {
		name string
		goos string
		env  func(string) string
		want bool
	}{
		{"mac", "darwin", env("WAYLAND_DISPLAY", "wayland-0"), true},
		{"windows", "windows", env(), true},
		{"x11", "linux", env("XDG_SESSION_TYPE", "x11", "DISPLAY", ":0"), true},
		{"wayland", "linux", env("XDG_SESSION_TYPE", "wayland", "WAYLAND_DISPLAY", "wayland-0"), false},
		{"wayland by its display alone", "linux", env("WAYLAND_DISPLAY", "wayland-0"), false},
		{"xwayland by GDK_BACKEND", "linux", env("XDG_SESSION_TYPE", "wayland", "GDK_BACKEND", "x11"), true},
	} {
		if got := keepsPlace(c.goos, c.env); got != c.want {
			t.Errorf("%s: keepsPlace = %v, want %v", c.name, got, c.want)
		}
	}
}
