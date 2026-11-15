package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// claude-desktop.tiers.json is read as no pick when it doesn't parse, and
// picking one tier wrote that over every other tier's pick (#2263): what
// was there is copied aside first.
func TestUnreadDesktopTiersKeptAside(t *testing.T) {
	for _, was := range []string{`{"opus": "fake/m1", "sonn`, `[]`} {
		t.Run(was, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			p := desktopTiersPath()
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(was), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SetDesktopTier("haiku", "fake/m2"); err != nil {
				t.Fatal(err)
			}
			bad, _ := filepath.Glob(p + ".bad-*")
			if len(bad) != 1 {
				t.Fatalf("kept aside: %v", bad)
			}
			if b, _ := os.ReadFile(bad[0]); string(b) != was {
				t.Fatalf("kept aside as %q, want %q", b, was)
			}
			if DesktopTiers()["haiku"] != "fake/m2" {
				t.Fatalf("the pick wasn't kept: %v", DesktopTiers())
			}
		})
	}
}
