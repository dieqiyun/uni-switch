package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/lastgood"
)

func dockHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	lastgood.Reset()
	t.Cleanup(lastgood.Reset)
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A settings file written before With window became the default (no
// dockSettled) stored Hide as nothing, so one with no Dock choice is read,
// and written back, as Hide: the new default never moves a user who chose
// Hide into the Dock. A choice the file has is kept as it is.
func TestOlderSettingsKeepHide(t *testing.T) {
	for _, c := range []struct {
		file       string
		dock, wind bool
		never      bool
	}{
		{`{"theme":"dark"}`, false, false, true},
		{`{}`, false, false, true},
		{`{"dockWindow":true}`, false, true, false},
		{`{"dock":true}`, true, false, false},
		{`{"dockNever":true}`, false, false, true},
	} {
		dockHome(t)
		if err := os.WriteFile(Path(), []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := LoadChecked()
		if err != nil {
			t.Fatal(err)
		}
		if s.Dock != c.dock || s.DockWindow != c.wind || s.DockNever != c.never || !s.DockSettled {
			t.Fatalf("%s read as dock %v window %v never %v settled %v", c.file, s.Dock, s.DockWindow, s.DockNever, s.DockSettled)
		}
		if err := Save(s); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(Path())
		var w Settings
		if err := json.Unmarshal(b, &w); err != nil {
			t.Fatal(err)
		}
		if w.DockNever != c.never || !w.DockSettled {
			t.Fatalf("%s written back as %s", c.file, b)
		}
		// read again, it stays: the migration runs once
		if s, _ = LoadChecked(); s.DockNever != c.never {
			t.Fatalf("%s changed on a second read: never %v", c.file, s.DockNever)
		}
	}
}

// A new install (no settings file) has With window: no Hide, and what it
// saves says so, so the next read doesn't take it for an older file.
func TestNewInstallFollowsWindow(t *testing.T) {
	dockHome(t)
	os.Remove(Path())
	s, err := LoadChecked()
	if err != nil {
		t.Fatal(err)
	}
	if s.Dock || s.DockNever || !s.DockSettled {
		t.Fatalf("a new install: dock %v never %v settled %v", s.Dock, s.DockNever, s.DockSettled)
	}
	s.Theme = "dark"
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if s, _ = LoadChecked(); s.DockNever || !s.DockSettled {
		t.Fatalf("a new install's first save read back as never %v settled %v", s.DockNever, s.DockSettled)
	}
}
