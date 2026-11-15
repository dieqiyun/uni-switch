package library

import (
	"path/filepath"
	"testing"
)

// A glance at the page leaves out what takes reading the agents' skill
// folders through, and says so; the whole read has it.
func TestGlanceLeavesOutTheAgentsSkills(t *testing.T) {
	h := sandbox(t)
	for _, d := range []string{".claude/skills/deck", ".codex/skills/deck"} {
		skill(t, filepath.Join(h, d), "deck", "Slides")
	}
	g, err := Glance(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Partial || g.FoundSkills == nil || len(g.FoundSkills) != 0 {
		t.Fatalf("glance: partial %v, found %+v", g.Partial, g.FoundSkills)
	}
	if len(g.Agents) == 0 {
		t.Fatal("a glance has the agents")
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Partial || len(v.FoundSkills) != 1 {
		t.Fatalf("read: partial %v, found %+v", v.Partial, v.FoundSkills)
	}
}
