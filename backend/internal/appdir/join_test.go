package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

// uni-switch's folders and the files named from them are joined once, not on
// every read of a file, and a folder changed since is seen at once.
func TestFoldersJoinedOnce(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	r := root(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(r, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(r, "cache"))
	t.Cleanup(func() { UseExecutable("") })
	UseExecutable("")

	for name, c := range map[string]struct {
		call func()
		env  string
	}{
		"Config":   {func() { Config() }, "XDG_CONFIG_HOME"},
		"Cache":    {func() { Cache() }, "XDG_CACHE_HOME"},
		"JoinFile": {func() { JoinFile(Cache(), "openai", ".json") }, "XDG_CACHE_HOME"},
	} {
		c.call()
		// Windows reads the current UTF-16 environment on every Getenv.
		// Only path construction must add no allocations beyond that read.
		reads := testing.AllocsPerRun(20, func() { os.Getenv(c.env) })
		if n := testing.AllocsPerRun(20, c.call); n > reads {
			t.Errorf("%s with the folders unchanged: %.0f allocations beyond environment reads", name, n-reads)
		}
	}
	if got, want := JoinFile(Join(Cache(), "models"), "openai", ".json"), filepath.Join(r, "cache", "uni-switch", "models", "openai.json"); got != want {
		t.Errorf("JoinFile = %q, want %q", got, want)
	}

	t.Setenv("XDG_CACHE_HOME", filepath.Join(r, "other"))
	if got, want := Cache(), filepath.Join(r, "other", "uni-switch"); got != want {
		t.Errorf("a cache folder changed since: %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(homeVar(), filepath.Join(r, "home"))
	if got, want := Config(), filepath.Join(r, "home", ".config", "uni-switch"); got != want {
		t.Errorf("under the home folder: %q, want %q", got, want)
	}
}
