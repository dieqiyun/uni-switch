package testenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A bun on PATH older than the oldest uni-switch runs plugins on skips the
// test that asked for it, rather than failing it on Bun's own
// differences (#1600: Bun 1.3 put :80 in a proxy's request line); MinBun
// itself and newer ones, a canary among them, run it.
func TestBunOlderThanMinSkips(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake bun is a shell script")
	}
	for _, c := range []struct {
		version string
		skips   bool
	}{
		{"1.3.0", true},
		{"1.4.1", true},
		{"0.9.9", true},
		{MinBun, false},
		{"1.4.3-canary.20261010.1", false},
		{"1.10.0", false},
		{"2.0.0", false},
		{"", false}, // said no version: unknown, so it runs
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bun"), []byte("#!/bin/sh\necho '"+c.version+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		var got string
		if !t.Run(c.version, func(t *testing.T) { got = Bun(t) }) {
			t.Fatalf("bun %q failed the test", c.version)
		}
		if skipped := got == ""; skipped != c.skips {
			t.Errorf("bun %q: skipped %v, want %v", c.version, skipped, c.skips)
		}
	}
}
