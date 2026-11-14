package filememo

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A read was asked for each model of each provider a poll lists, and each
// joined its kind and path into a new string to look the file up, as Look
// did for every file kept: 33 MB of what twenty window polls allocated. A
// read of a file found unchanged while held, and a Look, allocate nothing.
func TestHeldReadAndLookAllocateNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a folder long enough to be past any short string")
	os.MkdirAll(dir, 0o755)
	var paths []string
	parse := func(b []byte) (map[string]string, error) { return map[string]string{"k": string(b)}, nil }
	old := time.Now().Add(-time.Minute)
	for i := range 50 {
		p := filepath.Join(dir, "settings-"+strconv.Itoa(i)+".json")
		os.WriteFile(p, []byte("v"), 0o600)
		os.Chtimes(p, old, old)
		paths = append(paths, p)
	}
	release := Hold()
	defer release()
	for _, p := range paths {
		if m, err := Read("settings model ids", p, parse); err != nil || m["k"] != "v" {
			t.Fatal(m, err)
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		Read("settings model ids", paths[7], parse)
	}); n != 0 {
		t.Fatalf("a held read of a file found unchanged allocated %v times, want 0", n)
	}
	if n := testing.AllocsPerRun(100, func() { Look(paths[7]) }); n != 0 {
		t.Fatalf("Look over %d files allocated %v times, want 0", len(paths), n)
	}
	// and Look still has the file looked at again
	os.WriteFile(paths[7], []byte("w"), 0o600)
	os.Chtimes(paths[7], old.Add(time.Second), old.Add(time.Second))
	Look(paths[7])
	if m, _ := Read("settings model ids", paths[7], parse); m["k"] != "w" {
		t.Fatalf("a file written by another and Looked: %q", m["k"])
	}
}
