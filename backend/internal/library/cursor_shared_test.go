package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

// The Agents page calls cursor-agent Cursor CLI (@G_Z_Y on X), but the
// MCP servers and skills the Library gives it go to ~/.cursor, which the
// Cursor editor reads too: the Library's row is Cursor, and the agent
// itself is still Cursor CLI everywhere else.
func TestCursorLibraryRowIsCursor(t *testing.T) {
	sandbox(t)
	c := targetByID("cursor")
	if c == nil {
		t.Fatalf("no cursor target: %v", ids(Targets()))
	}
	if c.Agent.Name != "Cursor" {
		t.Errorf("the Library's row is %q, want Cursor", c.Agent.Name)
	}
	a, err := agent.Find("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Cursor CLI" {
		t.Errorf("the agent is %q after the Library named its row, want Cursor CLI", a.Name)
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, av := range v.Agents {
		if av.ID == "cursor" && av.Name != "Cursor" {
			t.Errorf("the Library's view names it %q", av.Name)
		}
	}
}

// Cursor CLI isn't detected for a ~/.cursor the Cursor editor made (no
// cli-config.json, no cursor-agent), but the editor reads that folder's
// mcp.json and skills: the Library still gives them, as Cursor's.
func TestCursorEditorAloneKeepsItsLibraryRow(t *testing.T) {
	h := sandbox(t)
	os.Remove(filepath.Join(h, ".cursor", "cli-config.json"))
	os.Remove(filepath.Join(h, "bin", "cursor-agent"))
	os.Remove(filepath.Join(h, "bin", "cursor-agent.exe"))
	write(t, filepath.Join(h, ".cursor", "argv.json"), "{}")
	a, err := agent.Find("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if a.Detected() {
		t.Fatal("Cursor CLI detected by the editor's ~/.cursor")
	}
	c := targetByID("cursor")
	if c == nil {
		t.Fatalf("the editor's ~/.cursor is no Library target: %v", ids(Targets()))
	}
	if c.Agent.Name != "Cursor" || c.MCP == nil || c.MCP.Path != filepath.Join(h, ".cursor", "mcp.json") || c.Skills != filepath.Join(h, ".cursor", "skills") {
		t.Errorf("cursor target: %+v", c)
	}
	// with no ~/.cursor at all there is nothing to give
	os.RemoveAll(filepath.Join(h, ".cursor"))
	if c := targetByID("cursor"); c != nil {
		t.Errorf("a cursor target with no ~/.cursor: %+v", c)
	}
}
