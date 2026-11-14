//go:build !windows

package update

import "context"

// CanRunElevated reports whether RunElevated can ask for an
// administrator here: only on Windows, where an agent's npm may live
// under Program Files.
func CanRunElevated() bool { return false }

// RunElevated is Windows' alone.
func RunElevated(context.Context, string, []string, ...string) ([]byte, error) {
	return nil, ErrNoAdmin
}
