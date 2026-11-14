package update

import "testing"

// Windows' PATH gets the app's folder first, once, its case and a trailing
// backslash aside (cliPlace).
func TestPathFirst(t *testing.T) {
	for _, c := range [][3]string{
		{"", `C:\M`, `C:\M`},
		{`C:\a;C:\b`, `C:\M`, `C:\M;C:\a;C:\b`},
		{`C:\a;c:\m\;C:\b;`, `C:\M`, `C:\M;C:\a;C:\b`},
		{`C:\M;C:\a`, `C:\M`, `C:\M;C:\a`},
	} {
		if got := pathFirst(c[0], c[1]); got != c[2] {
			t.Errorf("pathFirst(%q) = %q, want %q", c[0], got, c[2])
		}
	}
}
