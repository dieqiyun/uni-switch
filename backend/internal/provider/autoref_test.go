package provider

import (
	"os"
	"path/filepath"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// autoRefHome is a sandbox HOME with two providers serving Claude Opus 5.5,
// each spelling it its own way, and a models.dev catalog listing it as
// Anthropic spells it, with its window and reply limit.
func autoRefHome(t *testing.T) {
	t.Helper()
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"anthropic": {"models": {
	  "claude-opus-5.5": {"id":"claude-opus-5.5","name":"Claude Opus 5.5","limit":{"context":400000,"output":96000}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for id, m := range map[string]string{"a": "claude-opus-5-5", "b": "claude-opus-5.5"} {
		if err := Save(Provider{ID: id, Name: id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{m, "x"}}); err != nil {
			t.Fatal(err)
		}
	}
}
