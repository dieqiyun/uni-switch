package library

import (
	"os"
	"path/filepath"
	"testing"
)

// Bringing in every skill the agents have (JasonLeeForOnly on Discord: one
// click per skill found in ~/.agents): the ones found are brought in
// together, the shared folder's linked to and an agent's own moved in, and
// one that isn't there any more is said while the others go in all the same.
func TestImportSkills(t *testing.T) {
	h := sandbox(t)
	shared := filepath.Join(h, ".agents/skills")
	skill(t, filepath.Join(shared, "grilling"), "grilling", "Grill a plan")
	skill(t, filepath.Join(shared, "orca-cli"), "orca-cli", "Orca")
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "Mine")

	r, err := ImportSkills([]string{"grilling", "orca-cli", "notes", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Unimported) != 1 || r.Unimported[0].What != "skill:gone" {
		t.Errorf("unimported: %+v", r.Unimported)
	}
	v, _ := Read(nil)
	have := map[string]bool{}
	for _, s := range v.Skills {
		have[s.Name] = true
	}
	if !have["grilling"] || !have["orca-cli"] || !have["notes"] || len(v.FoundSkills) != 0 {
		t.Fatalf("skills: %+v, found: %+v", v.Skills, v.FoundSkills)
	}
	if _, err := os.Stat(filepath.Join(shared, "grilling/SKILL.md")); err != nil {
		t.Error("the shared folder's skill was moved")
	}
	if fi, err := os.Lstat(filepath.Join(h, ".claude/skills/notes")); err != nil || !linkEntry(fi) {
		t.Errorf("notes isn't linked from the library now: %v", err)
	}
	if _, err := ImportSkills([]string{"gone"}); err == nil {
		t.Error("none brought in, and no error")
	}
	if _, err := ImportSkills(nil); err == nil {
		t.Error("nothing asked for, and no error")
	}
}
