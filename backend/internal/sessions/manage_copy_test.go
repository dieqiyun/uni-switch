package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// Both a rename and the copy used across volumes must keep links as links,
// including relative and dangling links, without following their targets.
func TestMovePreservesSymlinks(t *testing.T) {
	for _, op := range []struct {
		name string
		run  func(string, string) error
	}{
		{"copy", copyAll},
		{"move", move},
	} {
		t.Run(op.name, func(t *testing.T) {
			t.Run("tree", func(t *testing.T) {
				dir := t.TempDir()
				from, to := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
				if err := os.MkdirAll(filepath.Join(from, "artifacts"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(from, "transcript.jsonl"), []byte("session"), 0o600); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(dir, "outside")
				if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
				links := []struct{ name, target string }{
					{"relative", "transcript.jsonl"},
					{"absolute", outside},
					{"directory", "artifacts"},
					{"dangling", "missing.jsonl"},
					{filepath.Join("artifacts", "nested"), filepath.Join("..", "transcript.jsonl")},
				}
				for _, link := range links {
					if err := os.Symlink(link.target, filepath.Join(from, link.name)); err != nil {
						t.Skip("no symlinks here:", err)
					}
				}
				if err := op.run(from, to); err != nil {
					t.Fatal(err)
				}
				for _, link := range links {
					if got, err := os.Readlink(filepath.Join(to, link.name)); err != nil || got != link.target {
						t.Errorf("link %s: %q, %v; want %q", link.name, got, err, link.target)
					}
				}
				if b, err := os.ReadFile(filepath.Join(to, "transcript.jsonl")); err != nil || string(b) != "session" {
					t.Fatalf("transcript: %q, %v", b, err)
				}
				if b, err := os.ReadFile(outside); err != nil || string(b) != "keep" {
					t.Fatalf("outside target changed: %q, %v", b, err)
				}
				if op.name == "move" {
					if _, err := os.Lstat(from); !os.IsNotExist(err) {
						t.Fatalf("source still exists: %v", err)
					}
				}
			})
			t.Run("root link", func(t *testing.T) {
				dir := t.TempDir()
				from, to := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
				if err := os.Symlink("missing", from); err != nil {
					t.Skip("no symlinks here:", err)
				}
				if err := op.run(from, to); err != nil {
					t.Fatal(err)
				}
				if got, err := os.Readlink(to); err != nil || got != "missing" {
					t.Fatalf("root link: %q, %v; want missing", got, err)
				}
				if op.name == "move" {
					if _, err := os.Lstat(from); !os.IsNotExist(err) {
						t.Fatalf("source still exists: %v", err)
					}
				}
			})
		})
	}
}

// Failure to create a link must be reported rather than silently skipped.
func TestCopyAllSymlinkConflict(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.Symlink("missing", from); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := os.WriteFile(to, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyAll(from, to); err == nil {
		t.Fatal("copy succeeded with a conflicting destination")
	}
	if got, err := os.Readlink(from); err != nil || got != "missing" {
		t.Fatalf("source link changed: %q, %v", got, err)
	}
	if b, err := os.ReadFile(to); err != nil || string(b) != "keep" {
		t.Fatalf("destination changed: %q, %v", b, err)
	}
}
