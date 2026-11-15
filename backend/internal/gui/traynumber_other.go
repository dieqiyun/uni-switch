//go:build !windows && !nogui

package gui

// Only Windows' tray draws a card's figure as its icon (traynumber.go).
func trayIconSize() int          { return 0 }
func trayTaskbarDark() bool      { return false }
func trayNumberFontFile() []byte { return nil }
