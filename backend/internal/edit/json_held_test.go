package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// GetJSON is asked of an agent's settings.json many times a page; a look
// at a file that hasn't changed neither copies it nor converts its
// comments again. It still sees a JSONC file's values, a BOM in front, an
// item by its members, and a write.
func TestGetJSONConvertsTheFileOnce(t *testing.T) {
	var b strings.Builder
	b.WriteString("\xef\xbb\xbf{\n  // the model\n  \"model\": \"fake/m1\",\n  \"items\": [\n")
	for i := range 4000 {
		fmt.Fprintf(&b, "    {\"name\": \"item-%d\", \"note\": \"some words to make it longer\"}, // %d\n", i, i)
	}
	b.WriteString("  ],\n}\n")
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if v, ok := GetJSON(path, "model"); !ok || v != "fake/m1" {
		t.Fatalf("model: %q %v", v, ok)
	}
	if v, ok := GetJSONRaw(path, "items.3999.name"); !ok || v != `"item-3999"` {
		t.Fatalf("last item: %q %v", v, ok)
	}

	const looks = 20
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for range looks {
		if v, _ := GetJSON(path, "model"); v != "fake/m1" {
			t.Fatalf("model: %q", v)
		}
	}
	runtime.ReadMemStats(&m1)
	if per, limit := (m1.TotalAlloc-m0.TotalAlloc)/looks, uint64(b.Len()/10); per > limit {
		t.Errorf("a look at a %d KB file allocates %d KB, over %d KB", b.Len()>>10, per>>10, limit>>10)
	}

	if err := SetJSON(path, KV{Path: "model", Value: "fake/m2"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetJSON(path, "model"); v != "fake/m2" {
		t.Errorf("after a write: %q, want fake/m2", v)
	}
}
