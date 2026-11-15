//go:build !windows && !nogui

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// lowWhileHidden: WebView2's memory target (memtarget_windows.go) has no
// counterpart in WKWebView or WebKitGTK that uni-switch can set.
func lowWhileHidden(*application.WebviewWindow) {}
