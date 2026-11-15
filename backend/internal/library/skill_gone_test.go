package library

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// スライム on Discord: a repository took some of its skills out, and
// Update all said each of them failed, with no way to take them all out
// but one by one. An update that reads the repository and finds a skill
// gone says so apart from failures, marks it, and every skill so marked
// goes at once, asked for. A repository that couldn't be fetched says
// nothing of whether its skills are gone.
func TestUpdateFindsSkillsRemovedUpstream(t *testing.T) {
	h := sandbox(t)
	removed, down := false, false
	var amu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amu.Lock()
		defer amu.Unlock()
		if down && strings.Contains(r.URL.Path, "other") {
			http.NotFound(w, r)
			return
		}
		files := map[string]string{
			"skills/pdf/SKILL.md":    "---\nname: pdf\ndescription: PDFs\n---\n",
			"skills/pdf/forms.md":    "forms",
			"skills/docx/SKILL.md":   "---\nname: docx\ndescription: Word\n---\n",
			"skills/slides/SKILL.md": "---\nname: slides\ndescription: Slides\n---\n",
			"x/SKILL.md":             "---\nname: x\ndescription: X\n---\n",
		}
		if removed {
			delete(files, "skills/docx/SKILL.md")
			delete(files, "skills/slides/SKILL.md")
			files["skills/pdf/forms.md"] = "forms two"
		}
		w.Write(tarball(t, files))
	}))
	defer srv.Close()
	old := tarballURL
	tarballURL = func(repo, ref string) string { return srv.URL + "/" + repo + "/" + ref }
	defer func() { tarballURL = old }()

	// repositories no other test probes: a probe is kept for an install
	// from it, so another test's owner/repo would be installed from here
	ok(t)(InstallSkills("owner/taken", []string{"skills/pdf", "skills/docx", "skills/slides"}, []string{"claude"}))
	ok(t)(InstallSkills("owner/taken-other", []string{"x"}, []string{"claude"}))
	if v, _ := Read(nil); len(v.Skills) != 4 {
		t.Fatalf("installed %d skills, want 4", len(v.Skills))
	}
	// slides is changed here: it is still said to be gone, and the page
	// says it was changed
	write(t, filepath.Join(skillDir("slides"), "SKILL.md"), "---\nname: slides\ndescription: mine\n---\n")
	amu.Lock()
	removed, down = true, true
	amu.Unlock()

	res, err := UpdateSkills()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Updated, []string{"pdf"}) {
		t.Errorf("updated %v", res.Updated)
	}
	if !slices.Equal(res.Gone, []string{"docx", "slides"}) {
		t.Errorf("gone %v", res.Gone)
	}
	// x's repository couldn't be fetched: a failure, not gone
	if len(res.Unupdated) != 1 || res.Unupdated[0].What != "skill:x" {
		t.Errorf("unupdated %+v", res.Unupdated)
	}
	v, _ := Read(nil)
	for _, s := range v.Skills {
		gone := s.Name == "docx" || s.Name == "slides"
		if (s.Gone != "") != gone {
			t.Errorf("%s: gone %q", s.Name, s.Gone)
		}
		if gone && (s.Check == nil || s.Check.Status != "gone" || !strings.Contains(s.Check.Error, "owner/taken")) {
			t.Errorf("%s: check %+v", s.Name, s.Check)
		}
		if s.Name == "slides" && !s.Edited {
			t.Error("slides, changed here, isn't said to be")
		}
	}
	// nothing is taken out by itself
	for _, n := range []string{"docx", "slides"} {
		if _, err := os.Stat(filepath.Join(skillDir(n), "SKILL.md")); err != nil {
			t.Errorf("%s went by itself: %v", n, err)
		}
	}

	// one named that isn't gone is kept, and said
	if _, err := RemoveGoneSkills([]string{"pdf"}); err == nil || !strings.Contains(err.Error(), "in its source again") {
		t.Errorf("pdf, still in its source: %v", err)
	}
	r := ok(t)(RemoveGoneSkills(nil))
	if len(r.Unremoved) != 0 {
		t.Errorf("unremoved %+v", r.Unremoved)
	}
	v, _ = Read(nil)
	var left []string
	for _, s := range v.Skills {
		left = append(left, s.Name)
	}
	if !slices.Equal(left, []string{"pdf", "x"}) {
		t.Errorf("left %v", left)
	}
	for _, n := range []string{"docx", "slides"} {
		if _, err := os.Lstat(filepath.Join(h, ".claude/skills", n)); !os.IsNotExist(err) {
			t.Errorf("claude still has %s", n)
		}
	}
	// the changed one's folder is with the backups, as Remove keeps it
	kept, _ := filepath.Glob(filepath.Join(BackupDir(), "*", "skills", "slides", "SKILL.md"))
	if len(kept) != 1 || !strings.Contains(read(t, kept[0]), "mine") {
		t.Errorf("slides in the backups: %v", kept)
	}
	if _, err := RemoveGoneSkills(nil); err == nil {
		t.Error("with nothing gone, nothing to say")
	}
}

// A skill marked gone that its repository has again is updated, and the
// mark goes.
func TestSkillBackUpstreamIsNotGone(t *testing.T) {
	sandbox(t)
	removed := false
	var amu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amu.Lock()
		defer amu.Unlock()
		files := map[string]string{"skills/a/SKILL.md": "---\nname: a\ndescription: A\n---\n", "skills/b/SKILL.md": "---\nname: b\ndescription: B\n---\n"}
		if removed {
			delete(files, "skills/b/SKILL.md")
		}
		w.Write(tarball(t, files))
	}))
	defer srv.Close()
	old := tarballURL
	tarballURL = func(repo, ref string) string { return srv.URL + "/" + repo + "/" + ref }
	defer func() { tarballURL = old }()

	ok(t)(InstallSkills("owner/back", []string{"skills/a", "skills/b"}, nil))
	amu.Lock()
	removed = true
	amu.Unlock()
	_, err := UpdateSkill("b", false)
	var gone *GoneError
	if !errors.As(err, &gone) || gone.Name != "b" || gone.Source != "owner/back" {
		t.Fatalf("update of a skill removed there: %v", err)
	}
	if l, _ := load(); l.skill("b").Gone == "" {
		t.Fatal("not marked")
	}
	amu.Lock()
	removed = false
	amu.Unlock()
	ok(t)(UpdateSkill("b", false))
	if l, _ := load(); l.skill("b").Gone != "" {
		t.Error("still marked once its source has it again")
	}
	if c := lastCheck("b"); c != nil && c.Status == "gone" {
		t.Errorf("check %+v", c)
	}
	if _, err := RemoveGoneSkills([]string{"b"}); err == nil {
		t.Error("b, back in its source, was taken out")
	}
}

// A skill of another git repository, removed there: an update and a check
// both say so, and it goes at once when asked.
func TestGitSkillRemovedUpstream(t *testing.T) {
	sandbox(t)
	addr, commit := gitRepo(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: A\n---\nv1\n",
		"skills/b/SKILL.md": "---\nname: b\ndescription: B\n---\n",
	})
	ok(t)(InstallSkills(addr, []string{"skills/a", "skills/b"}, []string{"claude"}))
	commit(map[string]string{"skills/b": gitRemoved, "skills/a/SKILL.md": "---\nname: a\ndescription: A\n---\nv2\n"})

	checks, err := CheckSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		want := map[string]string{"a": "update", "b": "gone"}[c.Name]
		if c.Status != want {
			t.Errorf("check of %s: %+v, want %s", c.Name, c, want)
		}
	}
	if l, _ := load(); l.skill("b").Gone == "" {
		t.Error("the check didn't mark b")
	}
	res := ok(t)(UpdateSkills())
	if !slices.Equal(res.Updated, []string{"a"}) || !slices.Equal(res.Gone, []string{"b"}) || len(res.Unupdated) != 0 {
		t.Errorf("updated %v, gone %v, unupdated %+v", res.Updated, res.Gone, res.Unupdated)
	}
	ok(t)(RemoveGoneSkills([]string{"b"}))
	if l, _ := load(); l.skill("b") != nil || l.skill("a") == nil {
		t.Errorf("after removing: %+v", l.Skills)
	}
}

// Only a source read whole says a skill is gone: one left out of a fetch
// as too big is there, and a folder that can't be read is unknown.
func TestGoneFromNeedsTheSourceRead(t *testing.T) {
	tmp := t.TempDir()
	write(t, filepath.Join(tmp, "skills/here/SKILL.md"), "---\nname: here\n---\n")
	write(t, filepath.Join(tmp, "skills/file"), "not a folder")
	if err := os.MkdirAll(filepath.Join(tmp, "skills/empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"skills/here":    false,
		"skills/missing": true,
		"skills/file":    true, // a file where the skill's folder was
		"skills/empty":   true, // a folder that is no skill any more
		"nothing/at/all": true,
	} {
		if got := goneFrom(tmp, path); got != want {
			t.Errorf("%s: gone %v, want %v", path, got, want)
		}
	}
	leftOut.Lock()
	leftOut.m[tmp] = []LeftOut{{Path: "skills/big", Size: 300 << 20}}
	leftOut.Unlock()
	t.Cleanup(func() { leftOut.Lock(); delete(leftOut.m, tmp); leftOut.Unlock() })
	if goneFrom(tmp, "skills/big") {
		t.Error("a skill left out as too big was called gone")
	}
	if len(leftOut.m[tmp]) != 1 {
		t.Error("asking forgot what was left out")
	}
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		return
	}
	locked := filepath.Join(tmp, "skills/locked")
	write(t, filepath.Join(locked, "SKILL.md"), "x")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if goneFrom(tmp, "skills/locked") {
		t.Error("a folder that couldn't be read was called gone")
	}
}
