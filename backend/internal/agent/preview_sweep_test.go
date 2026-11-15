package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDisconnectPreviewSweepsStaleSandboxes: a sandbox an earlier uni-switch
// was killed holding (#3136) is gone after the next preview, and a sandbox
// a live preview could be using is left alone.
func TestDisconnectPreviewSweepsStaleSandboxes(t *testing.T) {
	// a temp dir of the test's own: the sweep never reaches the real one
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, tmp)
	}
	stale := filepath.Join(tmp, "uni-switch-preview-sweeptest-stale")
	fresh := filepath.Join(tmp, "uni-switch-preview-sweeptest-fresh")
	for _, d := range []string{stale, fresh} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	home, _ := codexHome(t, "", "model = \"gpt-5.4\"\n")
	a := codex(home)
	if err := a.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DisconnectPreview(a, exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a stale sandbox is still there after a preview: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a fresh sandbox was swept: %v", err)
	}
}

// TestRemovePreviewRetriesWhileFilesAreHeld: a removal that fails while the
// dry run's processes still hold files is retried until it succeeds, and a
// removal that never succeeds is reported, not dropped (#3136).
func TestRemovePreviewRetriesWhileFilesAreHeld(t *testing.T) {
	dir := t.TempDir()
	real := previewRemoveAll
	var calls int
	previewRemoveAll = func(string) error {
		calls++
		if calls < 3 {
			return errors.New("file in use")
		}
		return real(dir)
	}
	t.Cleanup(func() { previewRemoveAll = real })
	if err := removePreview(dir); err != nil {
		t.Fatalf("removePreview gave up: %v", err)
	}
	if calls != 3 {
		t.Fatalf("removePreview tried %d times, want 3", calls)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("sandbox still there after removePreview: %v", err)
	}
}
