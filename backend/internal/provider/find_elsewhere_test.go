package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A uni-switch run in a shell whose XDG_CONFIG_HOME moves its files says so when
// a provider isn't found, naming the folder the app keeps them in (蒙面人 on
// Discord: `uni-switch provider refresh antigravity` found no Antigravity, which
// the window showed signed in); without the variable it says nothing more.
func TestFindSaysFilesAreElsewhere(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	app := filepath.Join(h, ".config", "uni-switch")
	if err := os.MkdirAll(app, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "logins.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "xdg"))
	_, err := findIn(nil, "antigravity")
	if err == nil || !strings.Contains(err.Error(), app) || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
		t.Fatalf("the app's folder isn't named: %v", err)
	}

	// the variable naming the app's own folder, or unset: nothing to say
	for _, x := range []string{filepath.Join(h, ".config"), ""} {
		t.Setenv("XDG_CONFIG_HOME", x)
		if _, err := findIn(nil, "antigravity"); err == nil || strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
			t.Fatalf("XDG_CONFIG_HOME=%q: %v", x, err)
		}
	}
}
