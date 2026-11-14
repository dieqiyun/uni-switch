package gui

import "testing"

// A closed window's webview goes on the second look in a row that finds it
// closed with lightweight mode on (#580), never while it is shown or the
// mode is off, and a window shown between looks starts over.
func TestLightStep(t *testing.T) {
	type look struct{ on, shown, release bool }
	for name, looks := range map[string][]look{
		"closed":         {{true, false, false}, {true, false, true}},
		"off":            {{false, false, false}, {false, false, false}, {false, false, false}},
		"shown":          {{true, true, false}, {true, true, false}, {true, true, false}},
		"shown between":  {{true, false, false}, {true, true, false}, {true, false, false}, {true, false, true}},
		"turned off":     {{true, false, false}, {false, false, false}, {true, false, false}, {true, false, true}},
		"again after it": {{true, false, false}, {true, false, true}, {true, false, false}, {true, false, true}},
	} {
		ticks := 0
		for i, l := range looks {
			var release bool
			ticks, release = lightStep(l.on, l.shown, ticks)
			if release != l.release {
				t.Fatalf("%s: look %d released %v", name, i, release)
			}
			if release {
				ticks = 0 // as lighten does once the window is let go
			}
		}
	}
}
