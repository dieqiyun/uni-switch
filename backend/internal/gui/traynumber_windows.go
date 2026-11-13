//go:build windows && !nogui

package gui

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// trayIconSize is the size Windows draws a tray icon at, in pixels at the
// display's scale (16 at 100%, 20 at 125%, 24 at 150%, 32 at 200%): Wails
// makes the icon at this size from the PNG it is given, so one drawn at it
// stays sharp.
func trayIconSize() int { return w32.GetSystemMetrics(w32.SM_CXSMICON) }

// trayTaskbarDark says whether the taskbar is dark: Windows' "default
// Windows mode" (SystemUsesLightTheme), which the taskbar follows and the
// apps' own mode doesn't change.
func trayTaskbarDark() bool { return w32.IsSystemCurrentlyDarkMode() }

// trayNumberFontFile is Segoe UI Semibold, Windows' own UI face, or its
// bold; nil when neither can be read.
func trayNumberFontFile() []byte {
	dir := os.Getenv("WINDIR")
	if dir == "" {
		dir = `C:\Windows`
	}
	for _, n := range []string{"seguisb.ttf", "segoeuib.ttf"} {
		if b, err := os.ReadFile(filepath.Join(dir, "Fonts", n)); err == nil {
			return b
		}
	}
	return nil
}
