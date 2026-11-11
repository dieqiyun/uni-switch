//go:build !windows && !nogui

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// TintTitleBar: the Mac's title bar is hidden in the page, and Linux's is
// the window manager's.
func (h *host) TintTitleBar([4]uint8, bool) bool { return false }

func windowChrome(string) (application.WindowsWindow, application.RGBA) {
	return application.WindowsWindow{}, application.RGBA{}
}
