package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Codex config.toml with a table for each of thousands of projects took
// ~0.2s of CPU to parse, and the Library parsed it on every visit (#1665):
// the file is parsed once until it changes, and a change to what entries
// gave doesn't reach the next caller.
func TestCodexEntriesParsedOncePerChange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	var b strings.Builder
	b.WriteString("[mcp_servers.a]\ncommand = \"x\"\nargs = [\"1\"]\n")
	for i := range 2000 {
		fmt.Fprintf(&b, "[projects.\"/p/%04d\"]\ntrust_level = \"trusted\"\n", i)
	}
	os.WriteFile(p, []byte(b.String()), 0o600)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	f := &mcpFile{Path: p, Format: fmtCodex}
	es, err := f.entries()
	if err != nil || es["a"]["command"] != "x" {
		t.Fatal(es, err)
	}
	es["a"]["command"] = "changed"
	es["a"]["args"].([]any)[0] = "changed"
	delete(es, "a")

	// asked again with the file unchanged: not parsed again
	n := entriesParsed.Load()
	for range 3 {
		es, _ = f.entries()
	}
	if d := entriesParsed.Load() - n; d != 0 {
		t.Fatalf("parsed %d more times with the file unchanged", d)
	}
	if es["a"]["command"] != "x" || es["a"]["args"].([]any)[0] != "1" {
		t.Fatalf("a caller's change reached the next one: %v", es["a"])
	}

	// a change is read
	os.WriteFile(p, []byte("[mcp_servers.b]\ncommand = \"z\"\n"), 0o600)
	if es, _ = f.entries(); es["b"]["command"] != "z" || es["a"] != nil {
		t.Fatalf("after a change: %v", es)
	}
	os.Remove(p)
	if es, err = f.entries(); err != nil || len(es) != 0 {
		t.Fatalf("no file: %v %v", es, err)
	}
}
