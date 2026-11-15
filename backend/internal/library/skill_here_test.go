package library

import (
	"encoding/json"
	"os"
	"path/filepath"

	"testing"
)

// at makes the sandbox h this computer again, after another sandbox was.
func at(t *testing.T, h string) {
	t.Helper()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("PATH", filepath.Join(h, "bin"))
}

func offView(t *testing.T, name string) SkillView {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no skill %s in %+v", name, v.Skills)
	return SkillView{}
}

func given(h, agentDir, name string) bool {
	_, err := os.Lstat(filepath.Join(h, agentDir, "skills", name))
	return err == nil
}

// A library.json from before, with no offHere, reads as on: nothing
// changes for a user who never turned a skill off here.
func TestSkillOffHereOldLibraryIsOn(t *testing.T) {
	h := sandbox(t)
	setUpLibrary(t, h)
	raw, err := os.ReadFile(path())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, s := range m["skills"].([]any) {
		if _, has := s.(map[string]any)["offHere"]; has {
			t.Fatalf("a skill never turned off here writes offHere: %s", raw)
		}
	}
	ok(t)(SkillAgents("pdf", []string{"claude"}))
	if !given(h, ".claude", "pdf") || offView(t, "pdf").OffHere {
		t.Fatal("an old library's skill isn't on")
	}
	if _, err := SkillHere("nope", false); err == nil {
		t.Fatal("turned off a skill the library hasn't")
	}
}
