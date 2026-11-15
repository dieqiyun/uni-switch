package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Joren on Discord: with skills given as copies, an agent's edit of one was
// undone at uni-switch's next start, the sync making the copy again from the
// library's. The newest edit goes to the library and every other agent
// instead; the library's own change wins when it is newer, and an edit that
// loses is kept with the backups.
func TestSkillCopyEditReachesTheOthers(t *testing.T) {
	h := sandbox(t)
	src := filepath.Join(h, "src/skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	write(t, filepath.Join(src, "pdf/forms.md"), "forms")
	ok(t)(InstallSkills(src, []string{"pdf"}, []string{"claude", "codex"}))
	ok(t)(SetSkillHow("", HowCopy))
	cl, cx := filepath.Join(h, ".claude/skills/pdf"), filepath.Join(h, ".codex/skills/pdf")
	tick := func() { time.Sleep(20 * time.Millisecond) }

	// Codex edits its copy, a file changed and one taken away
	tick()
	write(t, filepath.Join(cx, "SKILL.md"), "---\nname: pdf\ndescription: Edited in Codex\n---\n")
	if err := os.Remove(filepath.Join(cx, "forms.md")); err != nil {
		t.Fatal(err)
	}
	for range 2 { // a sync, and the one of the next start
		ok(t)(Sync())
		for _, p := range []string{cl, cx, filepath.Join(src, "pdf")} {
			if s := read(t, filepath.Join(p, "SKILL.md")); !strings.Contains(s, "Edited in Codex") {
				t.Fatalf("%s after a sync:\n%s", p, s)
			}
			if _, err := os.Stat(filepath.Join(p, "forms.md")); !os.IsNotExist(err) {
				t.Fatalf("%s still has forms.md: %v", p, err)
			}
		}
		if !ours(cl, "pdf") || !ours(cx, "pdf") || isLink(t, cx) {
			t.Fatal("the copies aren't uni-switch's any more")
		}
	}
	if !backedUp(t, "Read PDFs") {
		t.Error("the library's skill as it was isn't with the backups")
	}

	// Claude Code edits its copy, then the library's folder is edited by
	// hand after it: the library's wins, Claude Code's is kept aside
	tick()
	write(t, filepath.Join(cl, "SKILL.md"), "---\nname: pdf\ndescription: Edited in Claude\n---\n")
	tick()
	write(t, filepath.Join(src, "pdf/SKILL.md"), "---\nname: pdf\ndescription: Edited by hand\n---\n")
	ok(t)(Sync())
	for _, p := range []string{cl, cx} {
		if s := read(t, filepath.Join(p, "SKILL.md")); !strings.Contains(s, "Edited by hand") {
			t.Fatalf("%s after the library's change:\n%s", p, s)
		}
	}
	if !backedUp(t, "Edited in Claude") {
		t.Error("Claude Code's older edit was lost")
	}

	// both agents edit theirs: the newer one goes everywhere, the other is
	// kept aside
	tick()
	write(t, filepath.Join(cx, "SKILL.md"), "---\nname: pdf\ndescription: Codex again\n---\n")
	tick()
	write(t, filepath.Join(cl, "SKILL.md"), "---\nname: pdf\ndescription: Claude again\n---\n")
	ok(t)(Sync())
	for _, p := range []string{cl, cx, filepath.Join(src, "pdf")} {
		if s := read(t, filepath.Join(p, "SKILL.md")); !strings.Contains(s, "Claude again") {
			t.Fatalf("%s after two edits:\n%s", p, s)
		}
	}
	if !backedUp(t, "Codex again") {
		t.Error("Codex's older edit was lost")
	}

	// a copy only touched, the same bytes, takes nothing
	tick()
	now := time.Now()
	if err := os.Chtimes(filepath.Join(cx, "SKILL.md"), now, now); err != nil {
		t.Fatal(err)
	}
	ok(t)(Sync())
	if s := read(t, filepath.Join(src, "pdf/SKILL.md")); !strings.Contains(s, "Claude again") {
		t.Fatalf("library after a touch:\n%s", s)
	}
}

// backedUp is whether a SKILL.md with text in it is in the backups.
func backedUp(t *testing.T, text string) bool {
	t.Helper()
	found := false
	filepath.WalkDir(BackupDir(), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == "SKILL.md" {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), text) {
				found = true
			}
		}
		return nil
	})
	return found
}
