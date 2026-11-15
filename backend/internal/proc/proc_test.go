package proc

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every command uni-switch runs is made here, or the desktop app on Windows
// opens a console window for it again. internal/testenv is left out: only
// tests import it, never the app, and it can't import proc, whose own tests
// import it.
func TestNoCommandBypassesProc(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	bare := regexp.MustCompile(`\bexec\.Command(Context)?\(`)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || path == filepath.Join(root, "internal", "proc") || path == filepath.Join(root, "internal", "testenv")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bare.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d runs a command with os/exec; use proc.Command: %s", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
