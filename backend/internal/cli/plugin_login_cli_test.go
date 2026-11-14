package cli

import (
	"testing"
)

// noBrowser has the sign-in pages uni-switch opens go nowhere. It swaps the
// opener rather than putting a do-nothing open first on PATH: a script
// written now is a program macOS checks on its first run, which took 25 to
// 55 seconds while other test binaries ran, and ate the minute the plugin
// calls have (#1284).
func noBrowser(t *testing.T) {
	t.Helper()
	old := openInBrowser
	openInBrowser = func(string) {}
	t.Cleanup(func() { openInBrowser = old })
}
