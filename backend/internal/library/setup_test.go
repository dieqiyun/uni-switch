package library

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetupRestore(t *testing.T) {
	h := sandbox(t)
	src := filepath.Join(h, "src/skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	if _, err := ProbeSkills(src); err != nil {
		t.Fatal(err)
	}
	ok(t)(InstallSkills(src, []string{"pdf"}, []string{"claude"}))
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Agents: []string{"claude", "codex"}}))
	ok(t)(SaveServer("", Server{Name: "gone", Transport: "stdio", Command: "gone", Agents: []string{"codex"}}))
	shared, extra := "Use tabs.", "Codex only."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &shared, Agents: []string{"claude", "codex"}, Extra: map[string]*string{"codex": &extra}}))

	a, err := Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if a.Empty() || a.Summary() != "library: 2 servers, 1 skill, instructions" {
		t.Fatalf("summary %q", a.Summary())
	}

	// another setup: fs only for codex, the skill for codex, other
	// instructions for claude alone, no extra; a server added, one removed
	ok(t)(ServerAgents("fs", []string{"codex"}))
	ok(t)(SkillAgents("pdf", []string{"codex"}))
	other := "Use spaces."
	none := ""
	ok(t)(SaveInstructions(InstructionsChange{Shared: &other, Agents: []string{"claude"}, Extra: map[string]*string{"codex": &none}}))
	ok(t)(RemoveServer("gone"))
	ok(t)(SaveServer("", Server{Name: "new", Transport: "stdio", Command: "new", Agents: []string{"claude"}}))

	res := ok(t)(Restore(a))
	if !slices.Equal(res.Missing, []string{"mcp:gone"}) {
		t.Errorf("missing %v", res.Missing)
	}
	if res.Backup == "" {
		t.Error("nothing kept aside")
	}
	l, _ := load()
	if s := l.server("fs"); !slices.Equal(s.Agents, []string{"claude", "codex"}) {
		t.Errorf("fs: %v", s.Agents)
	}
	if s := l.server("new"); !slices.Equal(s.Agents, []string{"claude"}) {
		t.Errorf("a server added since lost its agents: %v", s.Agents)
	}
	if s := l.skill("pdf"); !slices.Equal(s.Agents, []string{"claude"}) {
		t.Errorf("pdf: %v", s.Agents)
	}
	cl, cx := read(t, filepath.Join(h, ".claude/CLAUDE.md")), read(t, filepath.Join(h, ".codex/AGENTS.md"))
	if !strings.Contains(cl, "Use tabs.") || strings.Contains(cl, "Use spaces.") {
		t.Errorf("claude:\n%s", cl)
	}
	if !strings.Contains(cx, "Use tabs.\n\nCodex only.") {
		t.Errorf("codex:\n%s", cx)
	}
	if _, err := os.Stat(filepath.Join(h, ".claude/skills/pdf/SKILL.md")); err != nil {
		t.Error("claude has no pdf again")
	}
	if _, err := os.Lstat(filepath.Join(h, ".codex/skills/pdf")); !os.IsNotExist(err) {
		t.Error("codex kept pdf")
	}
	if s := read(t, filepath.Join(h, ".claude.json")); !strings.Contains(s, `"fs"`) || !strings.Contains(s, `"new"`) {
		t.Errorf("claude's servers:\n%s", s)
	}
	// the library's own text it had is kept aside
	found := false
	es, _ := os.ReadDir(BackupDir())
	for _, e := range es {
		if read(t, filepath.Join(BackupDir(), e.Name(), "library", "instructions.md")) == "Use spaces.\n" {
			found = true
		}
	}
	if !found {
		t.Error("the library's instructions weren't kept aside")
	}

	// an empty library is an empty setup
	sandbox(t)
	if e, _ := Snapshot(); !e.Empty() {
		t.Errorf("empty: %+v", e)
	}
}
