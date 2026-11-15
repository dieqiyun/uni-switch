package settings

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// The menu bar shows several cards (TrayUsages); a settings file from before
// them, with the one TrayUsage, shows that one, and TrayUsage is kept as the
// first for an older uni-switch.
func TestTrayUsagesMigrate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"trayUsage":"claude|a@b.c","trayUsageEvery":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if !slices.Equal(s.TrayUsages, []string{"claude|a@b.c"}) || s.TrayUsage != "claude|a@b.c" {
		t.Fatalf("old file: %q %q", s.TrayUsages, s.TrayUsage)
	}
	// saved as it was read, then with more: TrayUsage follows the first
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if s = Load(); !slices.Equal(s.TrayUsages, []string{"claude|a@b.c"}) {
		t.Fatalf("saved as read: %q", s.TrayUsages)
	}
	s.TrayUsages = []string{" codex|x@y.z ", "claude|a@b.c", "codex|x@y.z", ""}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	s = Load()
	if !slices.Equal(s.TrayUsages, []string{"codex|x@y.z", "claude|a@b.c"}) || s.TrayUsage != "codex|x@y.z" {
		t.Fatalf("several: %q %q\n%s", s.TrayUsages, s.TrayUsage, b)
	}
	if !strings.Contains(string(b), `"trayUsage": "codex|x@y.z"`) {
		t.Fatalf("first not kept for an older uni-switch:\n%s", b)
	}
	// all turned off, though the page still sends the first it was drawn with
	s.TrayUsages = []string{}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if s = Load(); len(s.TrayUsages) != 0 || s.TrayUsage != "" {
		t.Fatalf("off: %q %q", s.TrayUsages, s.TrayUsage)
	}
	// and none of either is none
	if err := Save(Settings{}); err != nil {
		t.Fatal(err)
	}
	if s = Load(); s.TrayUsages != nil || s.TrayUsage != "" {
		t.Fatalf("none: %q %q", s.TrayUsages, s.TrayUsage)
	}
}
