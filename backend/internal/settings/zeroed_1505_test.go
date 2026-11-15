package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/lastgood"
)

// A settings.json a crash left at its length, every byte zero (#1505), is
// read from its last good generation, and saved over once kept aside.
func TestZeroedSettingsReadFromBackup(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	lastgood.Reset()
	t.Cleanup(lastgood.Reset)
	for _, theme := range []string{"dark", "light"} {
		if err := Save(Settings{Theme: theme}); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(Path(), make([]byte, 1873), 0o600)
	if got := Load().Theme; got != "dark" {
		t.Fatalf("a zeroed settings.json read with theme %q, want the backup's", got)
	}
	if len(lastgood.Recovered()) != 1 {
		t.Fatalf("not noted: %+v", lastgood.Recovered())
	}
	if err := Save(Settings{Theme: "system"}); err != nil {
		t.Fatalf("saving over a zeroed file with a backup: %v", err)
	}
	if kept, _ := filepath.Glob(Path() + ".bad-*"); len(kept) != 1 {
		t.Fatalf("the zeroed file not kept aside: %v", kept)
	}
	if got := Load().Theme; got != "system" {
		t.Fatalf("after a save: %q", got)
	}
}
