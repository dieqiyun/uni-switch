package library

import (
	"os"
	"path/filepath"
	"testing"
)

// egoLite lays out iomect's ego lite (#2635) under h at version v: the
// framework's Versions/<v> holding the skill, Versions/Current, and the
// app's own links in ~/.local/share/ego, which it points at the new
// version when it updates.
func egoLite(t *testing.T, h, v string) (versions string) {
	t.Helper()
	versions = filepath.Join(h, "Applications/ego lite.app/Contents/Frameworks/ego Framework.framework/Versions")
	skill(t, filepath.Join(versions, v, "Resources/ego-skills/ego-browser"), "ego-browser", "ego "+v)
	share := filepath.Join(h, ".local/share/ego")
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	for link, to := range map[string]string{
		filepath.Join(versions, "Current"):         v,
		filepath.Join(share, "active_version_dir"): filepath.Join(versions, v),
		filepath.Join(share, "ego-skills"):         filepath.Join(share, "active_version_dir/Resources/ego-skills/ego-browser"),
	} {
		os.Remove(link)
		if err := os.Symlink(to, link); err != nil {
			t.Skip("no symlinks here:", err)
		}
	}
	return versions
}

// A skill an app keeps in a versioned folder, brought in, is the app's
// current one after the app updates (iomect, #2635): ~/.agents/skills/
// ego-browser -> ~/.local/share/ego/ego-skills -> active_version_dir/... ->
// Versions/0.5.1.13, and the update points active_version_dir at 0.6.0.12.
func TestImportedSkillFollowsAppUpdate(t *testing.T) {
	h := sandbox(t)
	egoLite(t, h, "0.5.1.13")
	shared := filepath.Join(h, ".agents/skills")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h, ".local/share/ego/ego-skills"), filepath.Join(shared, "ego-browser")); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSkill("ego-browser"); err != nil {
		t.Fatal(err)
	}
	egoLite(t, h, "0.6.0.12")
	if m, ok := readMeta(skillDir("ego-browser")); !ok || m.Description != "ego 0.6.0.12" {
		to, _ := os.Readlink(skillDir("ego-browser"))
		t.Errorf("after the update the library's ego-browser is %q (links to %s), want 0.6.0.12's", m.Description, to)
	}
	v, _ := Read(nil)
	if len(v.Skills) != 1 || v.Skills[0].Missing || len(v.FoundSkills) != 0 {
		t.Errorf("skills %+v, found %+v", v.Skills, v.FoundSkills)
	}
}

// An agent's link straight into Versions/<v> is brought in through
// Versions/Current, which an update moves on.
func TestImportedSkillThroughVersionsCurrent(t *testing.T) {
	h := sandbox(t)
	versions := egoLite(t, h, "0.5.1.13")
	shared := filepath.Join(h, ".agents/skills")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(versions, "0.5.1.13/Resources/ego-skills/ego-browser"), filepath.Join(shared, "ego-browser")); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSkill("ego-browser"); err != nil {
		t.Fatal(err)
	}
	egoLite(t, h, "0.6.0.12")
	if m, ok := readMeta(skillDir("ego-browser")); !ok || m.Description != "ego 0.6.0.12" {
		t.Errorf("after the update the library's ego-browser is %q, want 0.6.0.12's", m.Description)
	}
}

// A link to a folder that isn't versioned is linked to where it is, as
// before.
func TestImportedSkillPlainLink(t *testing.T) {
	h := sandbox(t)
	src := filepath.Join(h, "code/notes")
	skill(t, src, "notes", "Mine")
	if err := os.MkdirAll(filepath.Join(h, ".claude/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, filepath.Join(h, ".claude/skills/notes")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if _, err := ImportSkill("notes"); err != nil {
		t.Fatal(err)
	}
	if to, err := os.Readlink(skillDir("notes")); err != nil || realDir(to) != realDir(src) {
		t.Errorf("library's notes links to %q (%v), want %s", to, err, src)
	}
}
