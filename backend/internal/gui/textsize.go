package gui

import "math"

// The text size (Settings → Text size, Ctrl/Cmd +, − and 0) is each
// webview's own zoom, as a browser zooms a page: the page is laid out in
// fewer, larger CSS pixels, and everything it measures stays in them, so
// what it places by a measure (a menu under its button, the panel's
// height) is where it was. The windows around the pages grow instead.

// panelMax keeps the panel a drop-down, not most of the screen: longer
// content (the usage of many accounts) scrolls in it (#124). These are the
// page's CSS pixels; at a larger text size the panel is that much larger.
const panelWidth, panelMin, panelMax = 440, 220, 560

// The main window's smallest size, in the page's CSS pixels too.
const windowMinW, windowMinH = 560, 420

// zoomed is n CSS pixels in the window's points at zoom z.
func zoomed(n int, z float64) int { return int(math.Round(float64(n) * z)) }

// zoomOf is a text size in percent as the webviews' zoom factor; anything
// unset or out of reason is 1.
func zoomOf(percent int) float64 {
	if percent < 50 || percent > 300 {
		return 1
	}
	return float64(percent) / 100
}

// panelFrame is the panel's size, in points, for a page page CSS pixels
// tall at zoom z, never taller than room (the screen's height below the
// menu bar or above the taskbar; 0 when it isn't known).
func panelFrame(page int, z float64, room int) (w, h int) {
	top := zoomed(panelMax, z)
	if room > 0 && top > room {
		top = room
	}
	h = min(top, max(zoomed(panelMin, z), zoomed(page, z)))
	return zoomed(panelWidth, z), h
}

// windowMin is the main window's smallest size, in points, at zoom z: the
// page gets no fewer CSS pixels than at 100%, unless the screen (w, h; 0
// when unknown) is smaller than that.
func windowMin(z float64, w, h int) (int, int) {
	mw, mh := zoomed(windowMinW, z), zoomed(windowMinH, z)
	if w > 0 {
		mw = min(mw, max(windowMinW, w))
	}
	if h > 0 {
		mh = min(mh, max(windowMinH, h))
	}
	return mw, mh
}
