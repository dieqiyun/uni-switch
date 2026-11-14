package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// sessions-wsl.json is the listing of session files in WSL distros, kept
// so a stopped distro is not started to list it again. A file that doesn't
// parse is read as no listing, and the next list writes that over it.
func TestUnreadWSLSessionListKeptAside(t *testing.T) {
	p := wslListHome(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	half := `{"Ubuntu":{"home":"/home/me","files":[{"agent":"claude","key":"s1","path":"/home/me/.claude/projects/a/s1.jsonl","size":1,"mod":"2026-10-01T00:00:00Z"}]`
	if err := os.WriteFile(p, []byte(half), 0o644); err != nil {
		t.Fatal(err)
	}
	wslFiles("claude")
	wslSettle()
	kept := keptWSLAside(t, p)
	if string(kept) != half {
		t.Fatalf("the file kept aside isn't the one that was there: %s", kept)
	}
}

// JSON that isn't the listing map is not a listing either.
func TestUnreadWSLSessionListArrayKeptAside(t *testing.T) {
	p := wslListHome(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	arr := []byte(`[]`)
	if err := os.WriteFile(p, arr, 0o644); err != nil {
		t.Fatal(err)
	}
	wslFiles("claude")
	wslSettle()
	if kept := keptWSLAside(t, p); string(kept) != string(arr) {
		t.Fatalf("the file kept aside isn't the one that was there: %s", kept)
	}
}

func wslListHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("USERPROFILE", filepath.Join(dir, "home"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	prevHomes, prevOff, prevRun := WSLHomes, WSLOff, WSLRunning
	WSLHomes = func() []WSLHome { return nil }
	WSLOff = nil
	WSLRunning = nil
	t.Cleanup(func() {
		wslSettle()
		WSLHomes, WSLOff, WSLRunning = prevHomes, prevOff, prevRun
		Reset()
	})
	Reset()
	return wslListPath()
}

func keptWSLAside(t *testing.T, path string) []byte {
	t.Helper()
	matches, err := filepath.Glob(path + ".bad-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("kept aside %d copies, want 1: %v", len(matches), matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return b
}
