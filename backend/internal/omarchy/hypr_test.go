package omarchy

import (
	"strings"
	"testing"
)

func TestPanelAt(t *testing.T) {
	// 2528x960 at 1.6 is 1580x600 in layout coordinates; Omarchy's bar
	// takes 26 at the top. A second monitor to its right, turned, at 1.
	mons := []hyprMonitor{
		{Name: "Virtual-1", Width: 2528, Height: 960, Scale: 1.6, Reserved: [4]int{0, 26, 0, 0}},
		{Name: "DP-1", X: 1580, Width: 1920, Height: 1080, Scale: 1, Transform: 1},
	}
	for _, c := range []struct {
		name       string
		cx, cy     int
		mon        string
		x, y, room int
		ok         bool
	}{
		{"middle", 800, 10, "Virtual-1", 580, 32, 562, true},
		{"right edge", 1570, 10, "Virtual-1", 1580 - 6 - 440, 32, 562, true},
		{"left edge", 20, 10, "Virtual-1", 6, 32, 562, true},
		// from DP-1's corner, which Hyprland adds (#1785)
		{"turned monitor", 1700, 1500, "DP-1", 6, 6, 1920 - 12, true},
		{"off every monitor", -5, 10, "", 0, 0, 0, false},
	} {
		mon, x, y, room, ok := panelAt(440, c.cx, c.cy, mons)
		if mon != c.mon || x != c.x || y != c.y || room != c.room || ok != c.ok {
			t.Errorf("%s: %q %d,%d room %d %v; want %q %d,%d room %d %v", c.name, mon, x, y, room, ok, c.mon, c.x, c.y, c.room, c.ok)
		}
	}
}

// landed is where Hyprland 0.56 puts a floating window that its move rule
// places at x, y: from the corner of the window's monitor
// (CDefaultFloatingAlgorithm::newTarget, COMPUTED + MONITOR_POS). The rule
// names that monitor.
func landed(mons []hyprMonitor, mon string, x, y int) (gx, gy int) {
	for _, m := range mons {
		if m.Name == mon {
			return m.X + x, m.Y + y
		}
	}
	return x, y
}

// A click on the tray of a monitor that isn't at 0,0 opens the panel on
// that monitor, under the click and inside its work area. With the
// monitor's place counted in the move as well as by Hyprland, the
// reporter's panel at 3238,40 went to 5158,40, off every screen (#1785).
func TestPanelAtMonitorOffScreenOrigin(t *testing.T) {
	const width = 440
	for _, c := range []struct {
		name   string
		mons   []hyprMonitor
		cx, cy int
		gx, gy int // where the panel's corner lands across the layout
	}{
		// the reporter's: two 1920x1080 side by side, a bar of 34 on each
		{"right of two", []hyprMonitor{
			{Name: "DP-1", Width: 1920, Height: 1080, Scale: 1, Reserved: [4]int{0, 34, 0, 0}},
			{Name: "HDMI-A-1", X: 1920, Width: 1920, Height: 1080, Scale: 1, Reserved: [4]int{0, 34, 0, 0}},
		}, 3458, 17, 3238, 40},
		{"right of two, at its right edge", []hyprMonitor{
			{Name: "DP-1", Width: 1920, Height: 1080, Scale: 1, Reserved: [4]int{0, 34, 0, 0}},
			{Name: "HDMI-A-1", X: 1920, Width: 1920, Height: 1080, Scale: 1, Reserved: [4]int{0, 34, 0, 0}},
		}, 3835, 17, 3840 - 6 - width, 40},
		{"left of two, the other at 0,0", []hyprMonitor{
			{Name: "eDP-1", Width: 1920, Height: 1080, Scale: 1, Reserved: [4]int{0, 34, 0, 0}},
			{Name: "DP-2", X: -2560, Y: -360, Width: 2560, Height: 1440, Scale: 1, Reserved: [4]int{0, 34, 0, 0}},
		}, -1000, -350, -1000 - width/2, -360 + 40},
		{"below, scaled", []hyprMonitor{
			{Name: "eDP-1", Width: 2880, Height: 1800, Scale: 2, Reserved: [4]int{0, 30, 0, 0}},
			{Name: "DP-3", Y: 900, Width: 3840, Height: 2160, Scale: 1.5, Reserved: [4]int{0, 30, 0, 0}},
		}, 20, 910, 6, 900 + 36},
	} {
		mon, x, y, room, ok := panelAt(width, c.cx, c.cy, c.mons)
		if !ok {
			t.Fatalf("%s: not placed", c.name)
		}
		gx, gy := landed(c.mons, mon, x, y)
		if gx != c.gx || gy != c.gy {
			t.Errorf("%s: the panel lands at %d,%d (%q %d,%d), want %d,%d", c.name, gx, gy, mon, x, y, c.gx, c.gy)
		}
		// on the monitor clicked, inside its work area
		var on hyprMonitor
		for _, m := range c.mons {
			if w, h := m.size(); c.cx >= m.X && c.cx < m.X+w && c.cy >= m.Y && c.cy < m.Y+h {
				on = m
			}
		}
		w, h := on.size()
		if mon != on.Name || gx < on.X+on.Reserved[0] || gx+width > on.X+w-on.Reserved[2] || gy < on.Y+on.Reserved[1] || gy+room > on.Y+h-on.Reserved[3] {
			t.Errorf("%s: %d,%d %dx%d on %q isn't inside %q's work area", c.name, gx, gy, width, room, mon, on.Name)
		}
	}
}

func TestPanelRule(t *testing.T) {
	got := panelRule("uni-switch panel", "HDMI-A-1", 1318, 40)
	for _, want := range []string{`match = { title = "^uni-switch panel$" }`, `monitor = "HDMI-A-1", move = { 1318, 40 }`, `float = true`, `pin = true`} {
		if !strings.Contains(got, want) {
			t.Errorf("rule %s\nhas no %s", got, want)
		}
	}
	// no monitor known: no monitor named, rather than an empty one
	if got := panelRule("uni-switch panel", "", 6, 32); strings.Contains(got, "monitor") || !strings.Contains(got, "move = { 6, 32 }") {
		t.Errorf("rule with no monitor: %s", got)
	}
}

func TestRegexpQuote(t *testing.T) {
	for in, want := range map[string]string{
		"uni-switch panel": "uni-switch panel",
		"a.b(c)":           `a\.b\(c\)`,
		"x+y*":             `x\+y\*`,
	} {
		if got := regexpQuote(in); got != want {
			t.Errorf("regexpQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOutside(t *testing.T) {
	at, size := [2]int{1134, 32}, [2]int{440, 520}
	for _, c := range []struct {
		name string
		x, y int
		want bool
	}{
		{"on the panel", 1300, 300, false},
		{"on its top-left corner", 1134, 32, false},
		{"left of it", 900, 300, true},
		{"under it", 1300, 560, true},
		{"on the bar", 1400, 10, false},
		{"in the gap under the bar", 900, 28, true},
	} {
		if got := outside(c.x, c.y, at, size); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
