package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// A Codex config.toml keeps a table for each project it was opened in, and
// the agents' looks list its tables many times a page: a look at a file
// that hasn't changed makes no list of its own. A caller's append doesn't
// reach the list, and a write is seen.
func TestTOMLTablesListedOnceARead(t *testing.T) {
	var b strings.Builder
	b.WriteString("model = \"m\"\n\n[[skills]]\nname = \"a\"\n\n")
	for i := range 3000 {
		fmt.Fprintf(&b, "[projects.\"/Users/someone/project-%d\"]\ntrust_level = \"trusted\"\n\n", i)
	}
	b.WriteString("[desktop]\non = true\n")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	names, err := TOMLTables(path)
	if err != nil || len(names) != 3001 || names[3000] != "desktop" || slices.Contains(names, "skills") {
		t.Fatalf("%d names, last %q, %v", len(names), names[len(names)-1], err)
	}
	_ = append(names, "mine")
	if again, _ := TOMLTables(path); len(again) != 3001 {
		t.Fatalf("a caller's append reached the list: %d", len(again))
	}
	const looks = 20
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for range looks {
		TOMLTables(path)
	}
	runtime.ReadMemStats(&m1)
	// the list itself is 3001 × 16 bytes, 47 KB
	if per := (m1.TotalAlloc - m0.TotalAlloc) / looks; per > 4<<10 {
		t.Errorf("a look at %d tables allocates %d bytes", len(names), per)
	}
	if err := SetTOMLTable(path, "extra", KV{Path: "a", Value: "b"}); err != nil {
		t.Fatal(err)
	}
	if again, _ := TOMLTables(path); !slices.Contains(again, "extra") {
		t.Errorf("after a write the list has no extra")
	}
}
