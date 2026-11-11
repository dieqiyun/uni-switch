package library

import (
	"path/filepath"
	"strings"
	"testing"
)

// A patch list in flow style (#445) is read and written like a block one:
// the user's rows stay, uni-switch's server goes in as an insert of its own.
func TestDshFlowPatchList(t *testing.T) {
	h := sandbox(t)
	p := filepath.Join(h, ".dsh/profiles/web/cordis.patch.yml")
	write(t, p, "# Your patch layer for this dsh profile\n[ { id: some-plugin, disabled: false } ]\n")
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Agents: []string{"dsh"}}))
	s := read(t, p)
	for _, want := range []string{"# Your patch layer for this dsh profile\n- id: some-plugin\n  disabled: false\n", "- insert: # uni-switch\n    - id: uni-switch-mcp-fs\n", `serverName: "fs"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
}

// A patch list that starts with a YAML document start marker (---) is read
// and written like one without it, the marker kept at the head (#2667).
func TestDshDocStartPatchList(t *testing.T) {
	h := sandbox(t)
	p := filepath.Join(h, ".dsh/profiles/web/cordis.patch.yml")
	// the reporter's file (#2667), down to its quoting and trailing spaces
	write(t, p, "---\n- \n  id: \"ui-settings-general\"\n  name: \"@deepseek-ai/dsh-client-ui-settings-general\"\n  config: \n    welcomeNoticeVersion: \"2026-09-28.1\"\n")
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"}, Agents: []string{"dsh"}}))
	s := read(t, p)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("the document start marker lost:\n%s", s)
	}
	for _, want := range []string{"id: \"ui-settings-general\"", "- insert: # uni-switch\n    - id: uni-switch-mcp-fs\n"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
}

// Only one "---" at the left edge, before anything but comments, opens the
// list (#2667). One anywhere else, or a second document, still fails the
// file instead of being kept as if it were the list's.
func TestDshDocStartOnlyFirst(t *testing.T) {
	h := sandbox(t)
	p := filepath.Join(h, "cordis.patch.yml")
	for _, raw := range []string{
		"---\n- id: a\n",
		"--- \n- id: a\n",
		"--- # patches\n- id: a\n",
		"# patches\n---\n- id: a\n",
		"---\n# patches\n\n- id: a\n",
		"---\n[ { id: a } ]\n",
	} {
		write(t, p, raw)
		d, err := dshRead(p)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if len(d.items) != 1 || d.items[0][0] != "- id: a" || !strings.Contains(strings.Join(d.head, "\n"), "---") {
			t.Fatalf("%q: head %q items %q", raw, d.head, d.items)
		}
	}
	for _, raw := range []string{
		"---\n---\n- id: a\n",
		"- id: a\n---\n- id: b\n",
		"---\n- id: a\n---\n- id: b\n",
		"  ---\n- id: a\n",
		"----\n- id: a\n",
		"--- x\n- id: a\n",
		"[]\n---\n- id: a\n",
	} {
		write(t, p, raw)
		if _, err := dshRead(p); err == nil || !strings.Contains(err.Error(), "not a list of entries uni-switch can edit") {
			t.Fatalf("%q: want the file refused, got %v", raw, err)
		}
	}
}
