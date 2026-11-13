package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The rollouts of a busy Codex folder are stat()ed on several goroutines,
// and the list comes back as one walk gave it: in the walk's order, each
// with its own size, and without what is gone, a folder or a link.
func TestCodexFilesStatInOrder(t *testing.T) {
	dir := t.TempDir()
	day := filepath.Join(dir, "sessions", "2026", "09", "30")
	if err := os.MkdirAll(day, 0700); err != nil {
		t.Fatal(err)
	}
	var want []string
	for i := range 1500 {
		name := fmt.Sprintf("rollout-2026-09-30T10-00-00-t%04d.jsonl", i)
		p := filepath.Join(day, name)
		switch {
		case i%97 == 5:
			if err := os.Mkdir(p, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		case i%89 == 7 && runtime.GOOS != "windows":
			if err := os.Symlink(filepath.Join(day, "rollout-2026-09-30T10-00-00-t0000.jsonl"), p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, make([]byte, i), 0600); err != nil {
			t.Fatal(err)
		}
		want = append(want, p)
	}
	got := codexFilesIn(dir)
	if len(got) != len(want) {
		t.Fatalf("%d rollouts, want %d", len(got), len(want))
	}
	for i, f := range got {
		var n int
		fmt.Sscanf(filepath.Base(f.path), "rollout-2026-09-30T10-00-00-t%04d.jsonl", &n)
		if f.path != want[i] || f.size != int64(n) || f.mod.IsZero() || f.key != fmt.Sprintf("codex:t%04d", n) {
			t.Fatalf("rollout %d: %s size %d key %s, want %s size %d", i, f.path, f.size, f.key, want[i], n)
		}
	}
}
