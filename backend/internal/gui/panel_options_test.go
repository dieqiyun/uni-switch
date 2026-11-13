//go:build !nogui

package gui

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// #238: on Windows the frameless, translucent panel showed DWM's caption
// close X at its top right; its caption buttons are hidden there (no
// WS_SYSMENU), and left as they were on the Mac and Linux.
func TestPanelOptionsCaptionButtons(t *testing.T) {
	w := panelOptions("windows", "")
	if !w.Frameless || w.BackgroundType != application.BackgroundTypeTranslucent || !w.Windows.HiddenOnTaskbar {
		t.Fatalf("windows panel lost its frameless, translucent, off-taskbar options: %+v", w)
	}
	if w.CloseButtonState != application.ButtonHidden || w.MinimiseButtonState != application.ButtonHidden || w.MaximiseButtonState != application.ButtonHidden {
		t.Errorf("windows panel caption buttons = close %v min %v max %v, want all hidden", w.CloseButtonState, w.MinimiseButtonState, w.MaximiseButtonState)
	}
	if w.Windows.DisableFramelessWindowDecorations {
		t.Errorf("windows panel should keep its shadow and round corners")
	}
	for _, goos := range []string{"darwin", "linux"} {
		o := panelOptions(goos, "&theme=dark")
		if o.CloseButtonState != application.ButtonEnabled || o.MinimiseButtonState != application.ButtonEnabled || o.MaximiseButtonState != application.ButtonEnabled {
			t.Errorf("%s panel caption buttons changed: close %v min %v max %v", goos, o.CloseButtonState, o.MinimiseButtonState, o.MaximiseButtonState)
		}
		if o.URL != "/?mode=panel&theme=dark" || !o.Frameless || o.Mac.CornerRadius != 12 || o.Mac.Backdrop != application.MacBackdropTranslucent {
			t.Errorf("%s panel options changed: %+v", goos, o)
		}
	}
}

// #1803: on Linux a see-through panel (an RGBA GTK window under a
// transparent webview) left a fainter copy of the old scroll position's
// paint behind; the panel is a solid window there, as the main window is.
// The Mac and Windows keep their translucent panel.
func TestPanelSolidOnLinux(t *testing.T) {
	if o := panelOptions("linux", ""); o.BackgroundType != application.BackgroundTypeSolid || !o.Frameless || !o.AlwaysOnTop {
		t.Errorf("linux panel = background %v frameless %v on top %v, want a solid, frameless, on-top window", o.BackgroundType, o.Frameless, o.AlwaysOnTop)
	}
	for _, goos := range []string{"darwin", "windows"} {
		if o := panelOptions(goos, ""); o.BackgroundType != application.BackgroundTypeTranslucent {
			t.Errorf("%s panel background = %v, want translucent", goos, o.BackgroundType)
		}
	}
}

// #2141: over a full-screen app the menu bar icon showed the quick panel on
// the desktop Space it was first shown on. On the Mac the panel is on every
// Space, full-screen ones among them, as a menu bar extra's panel is, and
// doesn't also move to the active Space (AppKit refuses the two together).
func TestPanelOnEverySpaceOnMac(t *testing.T) {
	b := panelOptions("darwin", "").Mac.CollectionBehavior
	want := application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorFullScreenAuxiliary
	if b&want != want {
		t.Errorf("panel collection behaviour %d is not on every Space and over full-screen ones", b)
	}
	if b&(application.MacWindowCollectionBehaviorMoveToActiveSpace|application.MacWindowCollectionBehaviorFullScreenPrimary) != 0 {
		t.Errorf("panel collection behaviour %d moves to the active Space or goes full screen itself", b)
	}
	// shown without activating uni-switch: activated, the Mac took the user to
	// the Space of uni-switch's other windows, off the full-screen app's
	mac := panelOptions("darwin", "").Mac
	if mac.WindowClass != application.MacWindowClassPanel || !mac.PanelPreferences.NonActivating {
		t.Errorf("the panel is window class %d, non-activating %v; want a non-activating panel", mac.WindowClass, mac.PanelPreferences.NonActivating)
	}
}
