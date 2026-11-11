package library

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// After Bring in all (StringKe, JasonLeeForOnly on Discord): "Claude Code
// already has a skill of its own called impeccable", and nothing to do
// about it. An agent's folder by the library skill's name that holds the
// very same files — a copy CC Switch made into each agent, or one put back
// by what installed it — gives way to the library's, kept aside; one that
// differs is said, and settled either way from the warning.
func TestOwnSkillInTheWay(t *testing.T) {
	h := sandbox(t)
	claude := filepath.Join(h, ".claude/skills/impeccable")
	codex := filepath.Join(h, ".codex/skills/impeccable")
	gemini := filepath.Join(h, ".gemini/skills/impeccable")
	skill(t, claude, "impeccable", "Design")
	skill(t, codex, "impeccable", "Design")      // a byte copy
	skill(t, gemini, "impeccable", "Old design") // another
	write(t, filepath.Join(codex, ".DS_Store"), "finder")
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "Mine")

	v, _ := Read(nil)
	i := slices.IndexFunc(v.FoundSkills, func(f FoundSkill) bool { return f.Name == "impeccable" })
	if i < 0 {
		t.Fatalf("found: %+v", v.FoundSkills)
	}
	if f := v.FoundSkills[i]; !slices.Equal(f.Agents, []string{"claude"}) || !slices.Equal(f.Copies, []string{"codex"}) || !slices.Equal(f.Others, []string{"gemini"}) {
		t.Errorf("impeccable: %+v", f)
	}

	// brought in from the agents' own folders: nothing stands in the way
	r := ok(t)(ImportSkills([]string{"impeccable", "notes"}))
	if len(r.Unimported) != 0 {
		t.Fatalf("unimported: %+v", r.Unimported)
	}
	for _, p := range []string{claude, codex} {
		if !ours(p, "impeccable") {
			t.Errorf("%s isn't the library's", p)
		}
	}
	if agents := skillAgents(t, "impeccable"); !slices.Equal(agents, []string{"claude", "codex"}) {
		t.Errorf("agents: %v", agents)
	}
	if m, _ := filepath.Glob(filepath.Join(BackupDir(), "*", "codex", "skills", "impeccable", "SKILL.md")); len(m) != 1 {
		t.Errorf("codex's copy isn't kept aside: %v", m)
	}
	if read(t, filepath.Join(gemini, "SKILL.md")) == "" || linked(gemini) {
		t.Error("gemini's own was touched")
	}

	// what installed it puts a copy back into Claude Code: the library's
	// takes its place again, the copy kept
	os.Remove(claude)
	if err := copyDir(skillDir("impeccable"), claude); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(claude, ".DS_Store"), "x")
	ok(t)(Sync())
	if !ours(claude, "impeccable") {
		t.Error("claude's copy wasn't taken over")
	}
	if m, _ := filepath.Glob(filepath.Join(BackupDir(), "*", "claude", "skills", "impeccable", "SKILL.md")); len(m) != 1 {
		t.Errorf("claude's copy isn't kept aside: %v", m)
	}

	// given to Gemini, whose own differs: said, and left as it is
	r, err := SkillAgents("impeccable", []string{"claude", "codex", "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 1 || r.Problems[0].Agent != "gemini" || !r.Problems[0].Own || !strings.Contains(r.Problems[0].Error, "of its own") {
		t.Fatalf("problems: %+v", r.Problems)
	}
	if linked(gemini) || !strings.Contains(read(t, filepath.Join(gemini, "SKILL.md")), "Old design") {
		t.Error("gemini's own was touched")
	}

	// keep the agent's: Gemini is off the skill, its own stays, no problem
	ok(t)(KeepAgentSkill("impeccable", "gemini"))
	if agents := skillAgents(t, "impeccable"); slices.Contains(agents, "gemini") {
		t.Errorf("gemini still gets it: %v", agents)
	}
	if linked(gemini) || !strings.Contains(read(t, filepath.Join(gemini, "SKILL.md")), "Old design") {
		t.Error("gemini's own was touched")
	}

	// use the library's: Gemini's own is kept aside, the library's linked
	ok(t)(UseLibrarySkill("impeccable", "gemini"))
	if !ours(gemini, "impeccable") {
		t.Error("gemini hasn't the library's")
	}
	if !slices.Contains(skillAgents(t, "impeccable"), "gemini") {
		t.Error("gemini isn't on the skill")
	}
	m, _ := filepath.Glob(filepath.Join(BackupDir(), "*", "gemini", "skills", "impeccable", "SKILL.md"))
	if len(m) != 1 || !strings.Contains(read(t, m[0]), "Old design") {
		t.Errorf("gemini's own isn't kept aside: %v", m)
	}

	if _, err := UseLibrarySkill("nothing", "gemini"); err == nil {
		t.Error("no such skill, and no error")
	}
	if _, err := KeepAgentSkill("impeccable", "nobody"); err == nil {
		t.Error("no such agent, and no error")
	}
}

func skillAgents(t *testing.T, name string) []string {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Skills {
		if s.Name == name {
			return s.Agents
		}
	}
	t.Fatalf("no skill %s", name)
	return nil
}

func TestSameTree(t *testing.T) {
	d := t.TempDir()
	a, b := filepath.Join(d, "a"), filepath.Join(d, "b")
	skill(t, a, "x", "X")
	skill(t, b, "x", "X")
	write(t, filepath.Join(a, ".DS_Store"), "1")
	write(t, filepath.Join(b, ".git/HEAD"), "ref")
	write(t, filepath.Join(b, marker), "copied")
	if !sameTree(a, b) {
		t.Fatal("the same files aren't the same")
	}
	write(t, filepath.Join(b, "scripts/run.sh"), "echo ho\n") // same size
	if sameTree(a, b) {
		t.Error("other bytes are the same")
	}
	write(t, filepath.Join(b, "scripts/run.sh"), "echo hi\n")
	write(t, filepath.Join(b, "more.md"), "")
	if sameTree(a, b) {
		t.Error("another file is the same")
	}
	if sameTree(a, filepath.Join(d, "gone")) {
		t.Error("a folder that isn't there is the same")
	}
}

// A backup holding a skill folder set aside is its only copy: it is never
// pruned with the others.
func TestPruneKeepsSkillsSetAside(t *testing.T) {
	sandbox(t)
	write(t, filepath.Join(BackupDir(), "2000-01-01_00-00-00.000", "gemini", "skills", "x", "SKILL.md"), "mine")
	for i := range keepBackups + 3 {
		write(t, filepath.Join(BackupDir(), fmt.Sprintf("2026-01-01_00-00-%02d.000", i), "claude", "CLAUDE.md"), "")
	}
	pruneBackups()
	if _, err := os.Stat(filepath.Join(BackupDir(), "2000-01-01_00-00-00.000", "gemini", "skills", "x", "SKILL.md")); err != nil {
		t.Error("the skill set aside was pruned")
	}
	es, _ := os.ReadDir(BackupDir())
	if len(es) != keepBackups+1 {
		t.Errorf("%d backups", len(es))
	}
}
