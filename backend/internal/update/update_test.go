package update

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.2.0", false},
		{"0.1.0", "0.1.0", false},
		{"v0.10.0", "0.9.9", true},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		// semver §11: identifiers that are numbers compare as numbers, a
		// number comes before a word, and a shorter list before a longer one
		{"1.0.0-beta.10", "1.0.0-beta.9", true},
		{"1.0.0-beta.9", "1.0.0-beta.10", false},
		{"1.6.0-10", "1.6.0-9", true},
		{"1.0.0-alpha.beta", "1.0.0-alpha.1", true},
		{"1.0.0-alpha.1", "1.0.0-alpha", true},
		{"1.0.0-alpha", "1.0.0-alpha.1", false},
		{"1.0.0-rc.1", "1.0.0-beta.11", true},
		{"2.0.0", "2.0.0-beta.1", true},
		// build metadata plays no part
		{"1.0.0+build.2", "1.0.0", false},
		{"1.0.0", "1.0.0+build.2", false},
		{"1.0.1+build.1", "1.0.0", true},
		{"1.0.0-rc.2+b", "1.0.0-rc.1", true},
		{"0.2.0", "dev", false},
		{"0.2.0", "0bcb2cc-dirty", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestReleased(t *testing.T) {
	for v, want := range map[string]bool{"0.1.0": true, "v1.2.3": true, "dev": false, "0bcb2cc-dirty": false, "v0.1.0-3-g0bcb2cc": false} {
		if Released(v) != want {
			t.Errorf("Released(%q) != %v", v, want)
		}
	}
}

func TestHomebrew(t *testing.T) {
	for exe, want := range map[string]bool{
		"/opt/homebrew/Cellar/uni-switch/0.1.126/bin/uni-switch":              true,
		"/home/linuxbrew/.linuxbrew/Cellar/uni-switch/0.1.126/bin/uni-switch": true,
		"/usr/local/bin/uni-switch":                                           false,
		"/Applications/uni-switch.app/Contents/MacOS/uni-switch":              false,
	} {
		if Homebrew(exe) != want {
			t.Errorf("%s: %v", exe, !want)
		}
	}
}
