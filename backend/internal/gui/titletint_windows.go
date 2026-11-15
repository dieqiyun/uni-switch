//go:build windows && !nogui

package gui

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// TintTitleBar paints the window's title bar the page's own colour, dark
// or light as the page is, so the bar and the page read as one surface as
// the Mac's hidden title bar does; Windows' grey bar sat above the page as
// a strip of its own, and stayed light over a page set to dark.
func (h *host) TintTitleBar(c [4]uint8, dark bool) bool {
	if !w32.SupportsCustomThemes() {
		return false
	}
	application.InvokeSync(func() {
		w := h.main
		if w == nil { // let go (lightweight mode); its page tints it when made again
			return
		}
		hwnd := uintptr(w.NativeWindow())
		if hwnd == 0 {
			return
		}
		w32.SetTheme(hwnd, dark)
		w32.SetTitleBarColour(hwnd, uint32(c[0])|uint32(c[1])<<8|uint32(c[2])<<16)
		// what shows past the page's edge while the window is resized
		w.SetBackgroundColour(application.NewRGB(c[0], c[1], c[2]))
	})
	return true
}

// windowChrome is the window's title bar and background before its page
// has said what colour it is: the page's own --bg for the theme it will
// open in.
func windowChrome(theme string) (application.WindowsWindow, application.RGBA) {
	dark := theme == "dark" || theme != "light" && w32.IsCurrentlyDarkMode()
	light, night := application.NewRGBPtr(0xf4, 0xf4, 0xf6), application.NewRGBPtr(0x1a, 0x1a, 0x1e)
	opts := application.WindowsWindow{
		Theme: application.Light,
		CustomTheme: application.ThemeSettings{
			LightModeActive:   &application.WindowTheme{TitleBarColour: light},
			LightModeInactive: &application.WindowTheme{TitleBarColour: light},
			DarkModeActive:    &application.WindowTheme{TitleBarColour: night},
			DarkModeInactive:  &application.WindowTheme{TitleBarColour: night},
		},
	}
	bg := application.NewRGB(0xf4, 0xf4, 0xf6)
	if dark {
		opts.Theme, bg = application.Dark, application.NewRGB(0x1a, 0x1a, 0x1e)
	}
	return opts, bg
}
