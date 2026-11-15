package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// profiles.json zeroed by a crash (#1505) is its last good generation.
func TestZeroedProfilesReadFromBackup(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	os.MkdirAll(appdir.Config(), 0o700)
	for _, ps := range []map[string]Profile{
		{"work": {Fields: map[string]string{"codex.model": "a"}}},
		{"work": {Fields: map[string]string{"codex.model": "a"}}, "home": {Fields: map[string]string{"codex.model": "b"}}},
	} {
		if err := store(ps); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(Path(), make([]byte, 512), 0o600)
	ps, err := Load()
	if err != nil || len(ps) != 1 || ps["work"].Fields["codex.model"] != "a" {
		t.Fatalf("a zeroed profiles.json read as %v, %v", ps, err)
	}
}
