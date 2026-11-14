package appdir

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestHermesDefault: Hermes's default home is %LOCALAPPDATA%\hermes on
// Windows, AppData\Local\hermes under the home folder without LOCALAPPDATA,
// and ~/.hermes everywhere else (hermes_constants.
// _get_platform_default_hermes_home). uni-switch looked in ~/.hermes on Windows
// too, where Hermes and rtk's hook weren't (新v学员 on Discord).
func TestHermesDefault(t *testing.T) {
	home := filepath.Join("C:", "Users", "admin")
	local := filepath.Join(home, "AppData", "Local")
	for _, c := range []struct{ goos, local, want string }{
		{"windows", local, filepath.Join(local, "hermes")},
		{"windows", "", filepath.Join(home, "AppData", "Local", "hermes")},
		{"windows", filepath.Join("D:", "Local"), filepath.Join("D:", "Local", "hermes")},
		{"darwin", local, filepath.Join(home, ".hermes")},
		{"linux", "", filepath.Join(home, ".hermes")},
	} {
		if got := hermesDefault(c.goos, home, c.local); got != c.want {
			t.Errorf("%s, LOCALAPPDATA=%q: %q, want %q", c.goos, c.local, got, c.want)
		}
	}
}

// TestHermesHome: HERMES_HOME comes first; without it, the platform's
// default, LOCALAPPDATA read as this machine has it.
func TestHermesHome(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("HERMES_HOME", "")
	want := filepath.Join(home, ".hermes")
	if runtime.GOOS == "windows" {
		want = filepath.Join(local, "hermes")
	}
	if got := HermesHomeIn(home); got != want {
		t.Errorf("HermesHomeIn = %q, want %q", got, want)
	}
	if got := HermesDefaultIn(home); got != want {
		t.Errorf("HermesDefaultIn = %q, want %q", got, want)
	}
	custom := filepath.Join(home, "elsewhere")
	t.Setenv("HERMES_HOME", custom)
	if got := HermesHomeIn(home); got != custom {
		t.Errorf("HermesHomeIn with HERMES_HOME = %q, want %q", got, custom)
	}
	if got := HermesDefaultIn(home); got != want {
		t.Errorf("HermesDefaultIn with HERMES_HOME = %q, want %q", got, want)
	}
	// a relative one, which Hermes would take from its working folder,
	// reads as unset
	t.Setenv("HERMES_HOME", "rel")
	if got := HermesHomeIn(home); got != want {
		t.Errorf("HermesHomeIn with a relative HERMES_HOME = %q, want %q", got, want)
	}
}
