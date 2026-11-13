//go:build !windows && !linux

package uninstall

// unregisterScheme: the Mac reads uni-switch:// from the app's Info.plist.
func unregisterScheme(string) error { return nil }

// webData: the Mac's webview keeps the app's pages by its bundle id, in
// folders the system manages.
func webData() []string { return nil }
