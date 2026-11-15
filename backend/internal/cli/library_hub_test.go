package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// uni-switch library mcp router on|off (#2679): on, the agent is given uni-switch's
// one MCP server in place of the library's; off, each server's own again.
// Any other word is refused, and writes nothing. "hub", its name before,
// still works.
func TestLibraryMCPHub(t *testing.T) {
	h := t.TempDir()
	for k, v := range map[string]string{"HOME": h, "USERPROFILE": h, "XDG_CONFIG_HOME": filepath.Join(h, ".config"), "CLAUDE_CONFIG_DIR": "", "CODEX_HOME": "", "APPDATA": "", "LOCALAPPDATA": ""} {
		t.Setenv(k, v)
	}
	if err := os.MkdirAll(filepath.Join(h, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, ".claude/settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	names := func() []string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(h, ".claude.json"))
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		var out []string
		for k := range c.MCPServers {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	if err := libraryCmd([]string{"library", "mcp", "add", "fs", "npx", "-y", "fs", "agents=claude"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"fs"}) {
		t.Fatalf("before: %v", got)
	}
	if err := libraryCmd([]string{"library", "mcp", "router", "on"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"uni-switch"}) {
		t.Fatalf("router on: %v", got)
	}
	if err := libraryCmd([]string{"library", "mcp", "router", "yes"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("router yes: %v", err)
	}
	if got := names(); !slices.Equal(got, []string{"uni-switch"}) {
		t.Fatalf("after a word it doesn't know: %v", got)
	}
	// a server kept direct (#4382) is its own entry again, beside no hub
	// entry once no server is behind it; shared, behind the router again
	// (said with "hub", its name before)
	if err := libraryCmd([]string{"library", "mcp", "router", "fs", "direct"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"fs"}) {
		t.Fatalf("fs direct: %v", got)
	}
	if err := libraryCmd([]string{"library", "mcp", "router", "fs", "solo"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("router fs solo: %v", err)
	}
	if err := libraryCmd([]string{"library", "mcp", "hub", "fs", "shared"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"uni-switch"}) {
		t.Fatalf("fs shared: %v", got)
	}
	if err := libraryCmd([]string{"library", "mcp", "hub", "off"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"fs"}) {
		t.Fatalf("hub off: %v", got)
	}
}
