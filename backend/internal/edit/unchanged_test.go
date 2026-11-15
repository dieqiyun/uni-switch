package edit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A write of the bytes a file already holds leaves it alone: its mtime
// and inode stay, so a watcher isn't woken and an agent reading it keeps
// its file (#3870). Each format's setter is tried, then a real change.
func TestUnchangedWriteKeepsFile(t *testing.T) {
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, tc := range []struct {
		name, file string
		set        func(string, any) error
	}{
		{"toml table", "config.toml", func(p string, v any) error {
			return SetTOMLTable(p, "providers.uni-switch", KV{Path: "base_url", Value: v})
		}},
		{"toml key", "config.toml", func(p string, v any) error {
			return SetTOMLKey(p, "providers.uni-switch", "base_url", v)
		}},
		{"toml top", "config.toml", func(p string, v any) error {
			return SetTOMLTop(p, KV{Path: "model", Value: v})
		}},
		{"json", "settings.json", func(p string, v any) error {
			return SetJSON(p, KV{Path: "env.BASE_URL", Value: v})
		}},
		{"yaml", "config.yaml", func(p string, v any) error {
			return SetYAML(p, KV{Path: "model.base_url", Value: v})
		}},
		{"env", ".env", func(p string, v any) error {
			return SetEnvFile(p, KV{Path: "BASE_URL", Value: v})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), tc.file)
			if err := tc.set(p, "http://127.0.0.1:3425"); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(p)
			if err := tc.set(p, "http://127.0.0.1:3425"); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Stat(p)
			if !after.ModTime().Equal(old) || !os.SameFile(before, after) {
				t.Fatalf("same bytes rewrote the file: mtime %v (was %v), same file %v",
					after.ModTime(), old, os.SameFile(before, after))
			}
			if err := tc.set(p, "http://127.0.0.1:3426"); err != nil {
				t.Fatal(err)
			}
			if st, _ := os.Stat(p); st.ModTime().Equal(old) {
				t.Fatal("a real change wasn't written")
			}
		})
	}
}
