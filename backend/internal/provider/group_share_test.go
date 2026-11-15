package provider

import (
	"testing"
)

func shareHome(t *testing.T, ps ...Provider) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, p := range ps {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
}
