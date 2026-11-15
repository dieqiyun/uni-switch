package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Sessions read Codex's config.toml on every visit, and one with a table for
// each of thousands of projects took ~0.15s of CPU to parse (#1665): it is
// parsed again only when it changes.
func TestCodexConfigParsedOncePerChange(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.toml")
	old := time.Now().Add(-time.Hour)
	write := func(s string) {
		os.WriteFile(p, []byte(s), 0o600)
		os.Chtimes(p, old, old)
	}
	write("model_provider = \"aaaa\"\n")
	if got := CodexProviderIn(d); got != "aaaa" {
		t.Fatal(got)
	}
	write("model_provider = \"bbbb\"\n")
	if got := CodexProviderIn(d); got != "aaaa" {
		t.Fatalf("parsed again with the file unchanged by size and time: %q", got)
	}
	os.WriteFile(p, []byte("model_provider = \"cc\"\n"), 0o600)
	if got := CodexProviderIn(d); got != "cc" {
		t.Fatalf("after a change: %q", got)
	}
}
