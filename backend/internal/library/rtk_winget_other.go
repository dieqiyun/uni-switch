//go:build !windows

package library

// rtkWingetLocations: winget's records are Windows' alone.
func rtkWingetLocations() ([]string, bool) { return nil, false }
