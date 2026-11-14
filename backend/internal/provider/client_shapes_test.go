package provider

import (
	"net/http"
	"os"
	"testing"
)

// Two Codex windows each send their own X-Codex-Window-Id. Taken as part
// of Codex's shape, every request of one after the other's read, marshalled
// and synced client-shapes.json again (#3394).
func TestClientShapeWrittenOnceForTwoWindows(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clientShapes.Lock()
	clientShapes.written = nil
	clientShapes.Unlock()
	window := func(id, ua string) http.Header {
		return http.Header{
			"User-Agent":              {ua},
			"Originator":              {"codex_exec"},
			"X-Codex-Window-Id":       {id},
			"X-Stainless-Retry-Count": {"0"},
		}
	}
	const ua = "codex_exec/0.162.1 (Mac OS 26.2.0; arm64) kitty"
	SawClient(ClientCodex, window("a:0", ua))
	if clientShape(ClientCodex).Get("Originator") != "codex_exec" {
		t.Fatal("Codex's shape was not kept")
	}
	if err := os.Remove(clientShapesPath()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		SawClient(ClientCodex, window("b:0", ua))
		SawClient(ClientCodex, window("a:0", ua))
	}
	if _, err := os.Stat(clientShapesPath()); err == nil {
		t.Fatal("client-shapes.json was written again for another window's request")
	}
	SawClient(ClientCodex, window("b:0", "codex_exec/0.163.0 (Mac OS 26.2.0; arm64) kitty"))
	if got := clientShape(ClientCodex).Get("User-Agent"); got != "codex_exec/0.163.0 (Mac OS 26.2.0; arm64) kitty" {
		t.Fatalf("a new Codex version's User-Agent was not kept: %q", got)
	}
}

// /api/providers asks the shape of every relay it lists: the file is
// parsed once until it changes, and what a caller does to its copy is
// not what the next one gets.
func TestClientShapeParsedOnceUntilWritten(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clientShapes.Lock()
	clientShapes.written = nil
	clientShapes.Unlock()
	SawClient(ClientClaudeCode, http.Header{"User-Agent": {"claude-cli/2.1.0 (external, cli)"}})
	clientShape(ClientClaudeCode)
	parses := shapeParses.Load()
	for range 30 {
		h := clientShape(ClientClaudeCode)
		if h.Get("User-Agent") != "claude-cli/2.1.0 (external, cli)" {
			t.Fatalf("shape read back as %v", h)
		}
		h.Set("User-Agent", "changed")
	}
	if n := shapeParses.Load() - parses; n != 0 {
		t.Fatalf("30 asks parsed the file %d times, want 0", n)
	}
	SawClient(ClientClaudeCode, http.Header{"User-Agent": {"claude-cli/2.2.0 (external, cli)"}})
	if got := clientShape(ClientClaudeCode).Get("User-Agent"); got != "claude-cli/2.2.0 (external, cli)" {
		t.Fatalf("after a write: %q", got)
	}
}
