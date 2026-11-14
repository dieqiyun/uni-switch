//go:build !linux && !nogui

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// setTrayTip: Wails' SetTooltip does it on Windows; on the Mac, where it
// does nothing, the tooltip is set on the item's button itself
// (trayTipNative), or no hover there ever showed the totals or the quotas
// (NextCommit on Discord).
func setTrayTip(_ *application.SystemTray, tip string) bool { return trayTipNative(tip) }

// setTrayPassive: only Linux has Omarchy's bar to put the icon in instead.
func setTrayPassive(*application.SystemTray, bool) bool { return true }

// trayClick: Wails places the panel by the click itself here.
func trayClick(*application.SystemTray) (int, bool) { return 0, false }

// traySchemeLight, watchTrayScheme: only Linux's tray takes the bird in a
// colour of the desktop's (linuxTrayIcon).
func traySchemeLight() bool      { return false }
func watchTrayScheme(func(bool)) {}
