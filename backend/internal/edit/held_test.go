package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// #1665: a catalog sync holds what was read for its round, so an agent's
// files are looked at once a round; a write is still made from what is on
// the disk, never over what another app wrote inside the held second.
func TestHeldReadIsNotWhatAWriteIsMadeFrom(t *testing.T) {
	cases := []struct {
		name, file, start, theirs string
		get                       func(path string) (string, bool)
		set                       func(path string) error
		want                      []string
	}{
		{"json", "settings.json", `{"a": 1}`, `{"a": 1, "theirs": true}`,
			func(p string) (string, bool) { return GetJSON(p, "a") },
			func(p string) error { return SetJSON(p, KV{"ours", 2}) },
			[]string{`"theirs": true`, `"ours"`}},
		{"toml", "config.toml", "a = 1\n", "a = 1\ntheirs = true\n",
			func(p string) (string, bool) { return GetTOMLTop(p, "a") },
			func(p string) error { return SetTOMLTop(p, KV{"ours", 2}) },
			[]string{"theirs = true", "ours = 2"}},
		{"yaml", "config.yaml", "a: 1\n", "a: 1\ntheirs: true\n",
			func(p string) (string, bool) { return GetYAMLTop(p, "a") },
			func(p string) error { return SetYAMLTop(p, KV{"ours", 2}) },
			[]string{"theirs: true", "ours: 2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.file)
			old := time.Now().Add(-time.Hour)
			writeAged(t, path, c.start, old)
			release := filememo.Hold()
			defer release()
			if v, ok := c.get(path); !ok || v != "1" {
				t.Fatalf("get = %q, %v", v, ok)
			}
			// another app's write, past the time a stamp can't tell apart
			writeAged(t, path, c.theirs, old.Add(time.Minute))
			if v, ok := c.get(path); !ok || v != "1" {
				t.Fatalf("held get = %q, %v", v, ok)
			}
			if err := c.set(path); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(path)
			for _, w := range c.want {
				if !strings.Contains(string(b), w) {
					t.Fatalf("missing %q after the write:\n%s", w, b)
				}
			}
		})
	}
}

func writeAged(t *testing.T, path, text string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// A write that leaves a file as it is keeps what is held of every file:
// a sync writes each agent's files each round, almost always unchanged.
func TestUnchangedWriteKeepsWhatIsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	writeAged(t, path, `{"a": 1}`, time.Now().Add(-time.Hour))
	release := filememo.Hold()
	defer release()
	GetJSON(path, "a")
	g := filememo.Gen()
	if err := SetJSON(path, KV{"a", 1}); err != nil {
		t.Fatal(err)
	}
	if filememo.Gen() != g {
		t.Fatal("an unchanged write forgot what was held")
	}
	if err := SetJSON(path, KV{"a", 2}); err != nil {
		t.Fatal(err)
	}
	if filememo.Gen() == g {
		t.Fatal("a write that changed the file kept what was held")
	}
}
