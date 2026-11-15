package appdir

import (
	"path/filepath"
	"testing"
)

// Logs is where each system keeps an app's logs, and the data folder's
// logs portable: the app opened from the Finder wrote none (rayem1w on X).
func TestLogsOnEachSystem(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	t.Cleanup(func() { UseExecutable("") })
	UseExecutable("")
	r := root(t)
	home, err := Home()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOCALAPPDATA", filepath.Join(r, "local"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(r, "state"))
	for goos, want := range map[string]string{
		"darwin":  filepath.Join(home, "Library", "Logs", "uni-switch"),
		"windows": filepath.Join(r, "local", "uni-switch", "logs"),
		"linux":   filepath.Join(r, "state", "uni-switch"),
		"freebsd": filepath.Join(r, "state", "uni-switch"),
	} {
		if got := logsOn(goos); got != want {
			t.Errorf("%s: logs in %q, want %q", goos, got, want)
		}
	}

	// unset or relative, the system's default under the home
	for _, v := range []string{"", "relative"} {
		t.Setenv("LOCALAPPDATA", v)
		t.Setenv("XDG_STATE_HOME", v)
		if got, want := logsOn("windows"), filepath.Join(home, "AppData", "Local", "uni-switch", "logs"); got != want {
			t.Errorf("windows, LOCALAPPDATA %q: logs in %q, want %q", v, got, want)
		}
		if got, want := logsOn("linux"), filepath.Join(home, ".local", "state", "uni-switch"); got != want {
			t.Errorf("linux, XDG_STATE_HOME %q: logs in %q, want %q", v, got, want)
		}
	}

	exe := touch(t, filepath.Join(r, "port", "uni-switch"))
	data := filepath.Join(r, "port", "data")
	mkdir(t, data)
	UseExecutable(exe)
	for _, goos := range []string{"darwin", "windows", "linux"} {
		if got, want := logsOn(goos), filepath.Join(data, "logs"); got != want {
			t.Errorf("portable on %s: logs in %q, want %q", goos, got, want)
		}
	}
}
