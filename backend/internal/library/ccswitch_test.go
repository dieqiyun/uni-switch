package library

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// A skill the library links to in CC Switch's folder is updated from the
// GitHub repository CC Switch installed it from, and becomes the library's
// own; CC Switch's folder is left as it was.
func TestCCSwitchSkillUpdates(t *testing.T) {
	h := sandbox(t)
	ccs := filepath.Join(h, ".cc-switch")
	write(t, filepath.Join(ccs, "skills/pdf/SKILL.md"), "---\nname: pdf\ndescription: old\n---\n")
	write(t, filepath.Join(ccs, "skills/mine/SKILL.md"), "---\nname: mine\n---\n")
	db, err := sql.Open("sqlite", filepath.Join(ccs, "cc-switch.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE skills (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT, directory TEXT NOT NULL, repo_owner TEXT, repo_name TEXT, repo_branch TEXT DEFAULT 'main')`,
		`INSERT INTO skills VALUES ('1','pdf','','pdf','owner','repo','main'), ('2','mine','','mine',NULL,NULL,NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		w.Write(tarball(t, map[string]string{
			"skills/pdf/SKILL.md": "---\nname: pdf\ndescription: new\n---\n",
			"skills/pdf/forms.md": "forms",
		}))
	}))
	defer srv.Close()
	old := tarballURL
	tarballURL = func(repo, ref string) string { return srv.URL + "/" + repo + "/" + ref }
	defer func() { tarballURL = old }()

	ok(t)(InstallSkills(filepath.Join(ccs, "skills"), []string{"pdf", "mine"}, []string{"claude"}))
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	origins := map[string]string{}
	for _, s := range v.Skills {
		origins[s.Name] = s.Origin
	}
	if origins["pdf"] != "https://github.com/owner/repo/tree/main" || origins["mine"] != "" {
		t.Fatalf("origins %v", origins)
	}
	if _, err := UpdateSkill("mine", false); err == nil {
		t.Error("one CC Switch has no repository for was updated")
	}
	ok(t)(UpdateSkill("pdf", false))
	if asked[len(asked)-1] != "/owner/repo/main" {
		t.Errorf("asked %v", asked)
	}
	if s := read(t, filepath.Join(h, ".claude/skills/pdf/forms.md")); s != "forms" {
		t.Errorf("claude's pdf: %q", s)
	}
	if s := read(t, filepath.Join(ccs, "skills/pdf/SKILL.md")); !strings.Contains(s, "old") {
		t.Errorf("CC Switch's folder changed: %q", s)
	}
	if fi, err := os.Lstat(skillDir("pdf")); err != nil || linkEntry(fi) {
		t.Errorf("the library's pdf is still a link: %v", err)
	}
	l, _ := load()
	if s := l.skill("pdf"); s.Source == nil || s.Source.Kind != "github" || s.Source.Repo != "owner/repo" || s.Source.Path != "skills/pdf" {
		t.Errorf("source %+v", s.Source)
	}
	ok(t)(UpdateSkill("pdf", false)) // from GitHub now, like any other
}
