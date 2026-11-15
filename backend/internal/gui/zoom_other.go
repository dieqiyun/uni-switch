//go:build !darwin && !nogui && !(windows && arm64)

package gui

import "github.com/wailsapp/wails/v3/pkg/application"

// setPageZoom zooms w's page to z, as a browser zooms (WebView2's zoom
// factor, WebKitGTK's zoom level): the text size.
func setPageZoom(w *application.WebviewWindow, z float64) { w.SetZoom(z) }

// zoomsAfterLoad: the window takes its text size with its options (Zoom).
const zoomsAfterLoad = false
