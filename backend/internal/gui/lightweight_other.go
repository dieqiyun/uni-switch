//go:build !darwin && !nogui

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// dropWebView: closing a window ends its webview off the Mac.
func dropWebView(*application.WebviewWindow) {}
