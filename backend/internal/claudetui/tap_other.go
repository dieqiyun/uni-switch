//go:build !windows

package claudetui

// shortPath is path: only Windows has short names.
func shortPath(path string) string { return path }
