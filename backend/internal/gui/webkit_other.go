//go:build !linux

package gui

// webkitDefaults: only Linux's webview is WebKitGTK.
func webkitDefaults() {}

// webkitPlaysVideo: the other systems' webviews play video.
var webkitPlaysVideo = func() bool { return true }
