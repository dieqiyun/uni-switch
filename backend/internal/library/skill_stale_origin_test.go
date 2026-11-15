package library

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// writtenAt sets every file under dir as last written at.
func writtenAt(t *testing.T, dir string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, at, at)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// AzheRise on X: a skill installed from a GitHub repository was deleted,
// and another made under the same name in an agent's own skills folder
// was filed under that repository once brought into the library, not
// under This computer. The skills CLI's lock is by name and outlives the
// skill. Only the folder the CLI wrote is that repository's skill: one
// made since is the user's, in ~/.dsh/skills or in ~/.agents/skills,
// brought in now or before. A copy the CLI gave an agent stays grouped,
// edited later too.
func TestDeletedSkillsLockNotInherited(t *testing.T) {
	h := sandbox(t)
	installed := time.Now().Add(-2 * time.Hour).UTC()
	ts := installed.Format(time.RFC3339Nano)
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{
		"notes":{"source":"someone/skills","sourceType":"github","installedAt":"`+ts+`","updatedAt":"`+ts+`"},
		"shared":{"source":"someone/skills","sourceType":"github","installedAt":"`+ts+`","updatedAt":"`+ts+`"},
		"copied":{"source":"someone/skills","sourceType":"github","installedAt":"`+ts+`","updatedAt":"`+ts+`"},
		"linked":{"source":"someone/skills","sourceType":"github","installedAt":"`+ts+`","updatedAt":"`+ts+`"}}}`)
	// the reporter's: a new skill of the deleted one's name, in an
	// agent's own folder (DeepSeek Harness's ~/.dsh/skills)
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "Mine, made today")
	// one made in ~/.agents/skills under such a name
	skill(t, filepath.Join(h, ".agents/skills/shared"), "shared", "Mine too")
	dirLink(filepath.Join(h, ".agents/skills/shared"), filepath.Join(h, ".claude/skills/shared"))
	// the CLI's own: a copy it gave Claude Code (--copy), and one in
	// ~/.agents/skills linked from Claude Code, each written as it locked
	skill(t, filepath.Join(h, ".claude/skills/copied"), "copied", "The CLI's copy")
	writtenAt(t, filepath.Join(h, ".claude/skills/copied"), installed.Add(-2*time.Second))
	skill(t, filepath.Join(h, ".agents/skills/linked"), "linked", "The CLI's")
	writtenAt(t, filepath.Join(h, ".agents/skills/linked"), installed.Add(-time.Second))
	dirLink(filepath.Join(h, ".agents/skills/linked"), filepath.Join(h, ".claude/skills/linked"))

	ok(t)(ImportSkills([]string{"notes", "shared", "copied", "linked"}))
	want := map[string]string{"notes": "", "shared": "", "copied": "someone/skills", "linked": "someone/skills"}
	got := repoIn(t)
	for n, r := range want {
		if got[n] != r {
			t.Errorf("%s: repo %q, want %q", n, got[n], r)
		}
	}
	l := mustLoad(t)
	if f := l.skill("notes").From; f != "" {
		t.Errorf("notes was given From %q", f)
	}
	if f := l.skill("copied").From; f != "someone/skills" {
		t.Errorf("the CLI's copy wasn't given its From: %q", f)
	}

	// brought in before: the lock's repository was kept as From for the
	// one in ~/.agents/skills, and the moved-in copy had none
	l.skill("shared").From = "someone/skills"
	l.skill("copied").From = ""
	if err := l.save(); err != nil {
		t.Fatal(err)
	}
	got = repoIn(t)
	for n, r := range want {
		if got[n] != r {
			t.Errorf("as brought in before, %s: repo %q, want %q", n, got[n], r)
		}
	}

	// the CLI's copy, edited since: its other files are as the CLI wrote
	// them
	write(t, filepath.Join(skillDir("copied"), "SKILL.md"), "---\nname: copied\ndescription: edited\n---\n")
	if r := repoIn(t)["copied"]; r != "someone/skills" {
		t.Errorf("the edited copy: repo %q", r)
	}
}

// A lock that doesn't say when it wrote a skill groups by name as
// before: nothing tells the skill apart from another of its name.
func TestUndatedLockStillGroups(t *testing.T) {
	h := sandbox(t)
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{"notes":{"source":"someone/skills","sourceType":"github"}}}`)
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "N")
	ok(t)(ImportSkills([]string{"notes"}))
	if r := repoIn(t)["notes"]; r != "someone/skills" {
		t.Errorf("repo %q", r)
	}
}

// A group the user made keeps the skill put in it, not its name: a skill
// removed from the library and another of that name brought in later is
// not in the group.
func TestRemovedSkillLeavesItsGroup(t *testing.T) {
	h := sandbox(t)
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "Old")
	skill(t, filepath.Join(h, ".claude/skills/other"), "other", "Other")
	ok(t)(ImportSkills([]string{"notes", "other"}))
	ok(t)(GroupSkills("", "Work", []string{"notes", "other"}))
	ok(t)(RemoveSkill("notes"))
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "New")
	ok(t)(ImportSkills([]string{"notes"}))
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.SkillGroups) != 1 || len(v.SkillGroups[0].Skills) != 1 || v.SkillGroups[0].Skills[0] != "other" {
		t.Errorf("groups %+v", v.SkillGroups)
	}
}

// CC Switch keeps the skills it installs in its own folder, by name, and
// that folder outlives a copy of it deleted from the library: another
// skill of that name brought in later is the user's, not CC Switch's. It
// is neither filed under CC Switch's repository nor updated from there,
// which would put the repository's skill in place of the user's.
func TestCCSwitchNameNotInherited(t *testing.T) {
	h := sandbox(t)
	ccs := filepath.Join(h, ".cc-switch")
	write(t, filepath.Join(ccs, "skills/pdf/SKILL.md"), "---\nname: pdf\ndescription: CC Switch's\n---\n")
	write(t, filepath.Join(ccs, "skills/same/SKILL.md"), "---\nname: same\ndescription: CC Switch's\n---\n")
	db, err := sql.Open("sqlite", filepath.Join(ccs, "cc-switch.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE skills (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT, directory TEXT NOT NULL, repo_owner TEXT, repo_name TEXT, repo_branch TEXT DEFAULT 'main')`,
		`INSERT INTO skills VALUES ('1','pdf','','pdf','owner','repo','main'), ('2','same','','same','owner','repo','main')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	// the user's own pdf, and a copy CC Switch gave Claude Code
	skill(t, filepath.Join(h, ".claude/skills/pdf"), "pdf", "Mine")
	write(t, filepath.Join(h, ".claude/skills/same/SKILL.md"), "---\nname: same\ndescription: CC Switch's\n---\n")
	ok(t)(ImportSkills([]string{"pdf", "same"}))
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Skills {
		switch s.Name {
		case "pdf":
			if s.Origin != "" || s.Repo != "" {
				t.Errorf("the user's pdf: origin %q, repo %q", s.Origin, s.Repo)
			}
		case "same":
			if s.Origin == "" || s.Repo != "owner/repo" {
				t.Errorf("CC Switch's copy: origin %q, repo %q", s.Origin, s.Repo)
			}
		}
	}
	l := mustLoad(t)
	if updatable(l.skill("pdf")) {
		t.Error("the user's pdf can be updated from CC Switch's repository")
	}
}
