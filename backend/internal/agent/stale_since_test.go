package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A copy's start is now less how long it has run, two clock reads apart,
// so it moved by microseconds on each look. The Agents page keeps the
// state it was given, and a time that moved rewrote that file on every
// poll. Asked again, a copy that hasn't changed says the same start.
func TestStaleCopySinceSameEachLook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(staleOnWindows)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	cfg := filepath.Join(dir, "config.toml")
	list := filepath.Join(dir, "uni-switch-models.json")
	conf := "model = \"uni-switch/gpt-6-luna\"\nmodel_provider = \"uni-switch\"\nmodel_catalog_json = \"" + tomlString(list) + "\"\n\n" +
		"[model_providers.uni-switch]\nname = \"uni-switch\"\nbase_url = \"http://127.0.0.1:3425/v1\"\n"
	os.WriteFile(cfg, []byte(conf), 0o600)
	os.WriteFile(list, []byte(`{"models":[{"slug":"uni-switch/gpt-6-luna"}]}`), 0o644)

	// started two hours ago, half a second past a whole second, as the
	// process list tells it: how long it has run at the moment it is read
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Second).Add(500 * time.Millisecond)
	was := running
	t.Cleanup(func() { running = was })
	running = func(string) []runningProc {
		return []runningProc{{time.Since(start), "/Applications/Codex.app/Contents/Resources/codex app-server --listen stdio://"}}
	}
	a := codex(home)
	first := a.StaleCopies()
	if len(first) != 1 {
		t.Fatalf("the copy from before uni-switch's list: %+v", first)
	}
	for range 20 {
		time.Sleep(time.Millisecond)
		got := a.StaleCopies()
		if len(got) != 1 || !got[0].Since.Equal(first[0].Since) {
			t.Fatalf("asked again: %+v, first %+v", got, first)
		}
	}
	if d := start.Sub(first[0].Since); d < 0 || d >= time.Second {
		t.Fatalf("since %v, the copy started %v", first[0].Since, start)
	}
}
