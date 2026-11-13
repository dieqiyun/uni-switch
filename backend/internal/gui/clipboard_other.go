//go:build (!linux || !cgo || !gtk3) && !nogui

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// copyText: Wails' copy, as it was; only GTK 3 on Wayland needs a read
// back (#1881, clipboard_gtk3.go).
func copyText(app *application.App, text string) bool { return app.Clipboard.SetText(text) }
