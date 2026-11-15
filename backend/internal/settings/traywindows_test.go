package settings

import (
	"maps"
	"os"
	"slices"
	"testing"
)

// Which of a subscription's windows its menu bar card shows (TrayWindows,
// StrikingRaw on X): kept by provider and kind as the user picked them,
// each kind once, and a provider with none picked left out, as it shows
// every window; a file without the field shows every window, as before.
func TestTrayWindowsKept(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"trayUsages":["claude|a@b.c"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.TrayWindows != nil {
		t.Fatalf("a file from before: %v", s.TrayWindows)
	}
	s.TrayWindows = map[string][]string{"claude": {" 5h", "5h"}, "codex": {"7d", "5h"}, "kimi": {}, " ": {"5h"}}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"claude": {"5h"}, "codex": {"7d", "5h"}}
	if s = Load(); !maps.EqualFunc(s.TrayWindows, want, slices.Equal) {
		t.Fatalf("saved: %v", s.TrayWindows)
	}
	// every one taken back: none kept
	s.TrayWindows = map[string][]string{"claude": nil}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if s = Load(); s.TrayWindows != nil {
		t.Fatalf("taken back: %v", s.TrayWindows)
	}
}
