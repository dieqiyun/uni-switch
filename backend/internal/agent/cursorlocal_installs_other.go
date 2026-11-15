//go:build !windows

package agent

// cursorLocalInstalls: only Windows keeps a list of where programs are
// installed; elsewhere the folders in cursorLocalRoots are looked in.
func cursorLocalInstalls() []string { return nil }
