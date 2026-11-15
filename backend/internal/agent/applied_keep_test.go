package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// applied.json holds what uni-switch last set on every agent. A file that
// doesn't parse is read as no record, and the next record writes that
// over it: every other agent's record is copied aside first.
func TestUnreadAppliedKeptAside(t *testing.T) {
	stashHome(t)
	p := appliedPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	half := `{"claude":{"at":"2026-10-01T00:00:00Z","fields":{"model":"uni-switch/opus"}},"codex":{"at":"2026-10-01T00:00:00Z","fields":{"model":"uni-switch/gpt"}}`
	if err := os.WriteFile(p, []byte(half), 0o600); err != nil {
		t.Fatal(err)
	}
	record("pi", "model", "uni-switch/pi-model")
	kept := keptAside(t, p)
	if string(kept) != half {
		t.Fatalf("the file kept aside isn't the one that was there: %s", kept)
	}
	live, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `"uni-switch/pi-model"`) {
		t.Fatalf("applied.json after the record: %s", live)
	}

	// a file that parses is written over as before, nothing more kept
	good := `{"claude":{"at":"2026-10-01T00:00:00Z","fields":{"model":"uni-switch/opus"}}}`
	if err := os.WriteFile(p, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	record("pi", "model", "uni-switch/pi-model")
	if more, _ := filepath.Glob(p + ".bad-*"); len(more) != 1 {
		t.Fatalf("kept aside after a good write: %v", more)
	}
	live, err = os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `"uni-switch/opus"`) || !strings.Contains(string(live), `"uni-switch/pi-model"`) {
		t.Fatalf("applied.json after a good record: %s", live)
	}
}

// JSON that isn't the record map (an array, as a hand edit or another
// program might leave) is not a record of what uni-switch set either.
func TestUnreadAppliedArrayKeptAside(t *testing.T) {
	stashHome(t)
	p := appliedPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	arr := []byte(`[]`)
	if err := os.WriteFile(p, arr, 0o600); err != nil {
		t.Fatal(err)
	}
	record("pi", "model", "uni-switch/pi-model")
	if kept := keptAside(t, p); string(kept) != string(arr) {
		t.Fatalf("the file kept aside isn't the one that was there: %s", kept)
	}
}

// A copy that can't be made stops the write: the unread file stays.
func TestUnreadAppliedNotWrittenWhenItCantBeKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory without write permission still accepts a new name")
	}
	stashHome(t)
	p := appliedPath()
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	half := `{"claude":{"at":"2026-10-01T00:00:00Z","fields":{"model":"uni-switch/opus"}}`
	if err := os.WriteFile(p, []byte(half), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		f.Close()
		t.Skip("the filesystem does not enforce the read-only folder")
	}
	record("pi", "model", "uni-switch/pi-model")
	live, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(live) != half {
		t.Fatalf("applied.json = %q, want it left as %q", live, half)
	}
}

// wsl.json holds every distro's last probe. A file that doesn't parse is
// read as no distro, and the next save writes that over it.
func TestUnreadWSLConfigKeptAside(t *testing.T) {
	stashHome(t)
	p := wslStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	half := `{"Ubuntu":{"name":"Ubuntu","home":"/home/me","probe":2,"values":{"claude.model":"uni-switch/opus"}}`
	if err := os.WriteFile(p, []byte(half), 0o600); err != nil {
		t.Fatal(err)
	}
	saveFreshWSL(t)
	kept := keptAside(t, p)
	if string(kept) != half {
		t.Fatalf("the file kept aside isn't the one that was there: %s", kept)
	}
	live, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(live), `"Fresh"`) {
		t.Fatalf("wsl.json after the save: %s", live)
	}
}

func TestUnreadWSLConfigArrayKeptAside(t *testing.T) {
	stashHome(t)
	p := wslStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	arr := []byte(`[]`)
	if err := os.WriteFile(p, arr, 0o600); err != nil {
		t.Fatal(err)
	}
	saveFreshWSL(t)
	if kept := keptAside(t, p); string(kept) != string(arr) {
		t.Fatalf("the file kept aside isn't the one that was there: %s", kept)
	}
}

func saveFreshWSL(t *testing.T) {
	t.Helper()
	wsl.Lock()
	oldSeen, oldDirty, oldAt, oldRun := wsl.seen, wsl.dirty, wsl.at, wsl.runAt
	wsl.seen = map[string]*distro{"Fresh": {Name: "Fresh", Home: "/home/fresh", Probe: wslProbeVersion}}
	wsl.dirty = true
	wsl.Unlock()
	t.Cleanup(func() {
		wsl.Lock()
		wsl.seen, wsl.dirty, wsl.at, wsl.runAt = oldSeen, oldDirty, oldAt, oldRun
		wsl.Unlock()
	})
	wslSave()
}

func keptAside(t *testing.T, path string) []byte {
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
