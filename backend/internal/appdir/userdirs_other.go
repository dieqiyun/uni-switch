//go:build !windows

package appdir

// osKnownFolder has nothing off Windows.
func osKnownFolder(string) (path, profile string) { return "", "" }
