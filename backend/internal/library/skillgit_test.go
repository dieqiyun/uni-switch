package library

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sunset on Discord: skills from GitLab, and from a company's own GitLab
// on its intranet. What the user types is read as a git repository, with
// a GitLab page's ref and folder; what git would take as an option, or a
// transport that runs a command, is refused.
func TestGitSourceReads(t *testing.T) {
	for _, c := range []struct{ in, repo, ref, path string }{
		{"https://gitlab.com/group/skills", "https://gitlab.com/group/skills", "", ""},
		{"https://gitlab.com/group/skills.git", "https://gitlab.com/group/skills.git", "", ""},
		{"https://git.corp.example/team/sub/deep/skills/", "https://git.corp.example/team/sub/deep/skills", "", ""},
		{"https://git.corp.example/team/sub/skills/-/tree/main/skills/pdf", "https://git.corp.example/team/sub/skills", "main", "skills/pdf"},
		{"https://gitlab.com/g/r/-/tree/HEAD", "https://gitlab.com/g/r", "", ""},
		{"https://gitlab.com/g/r/-/blob/v1.2/skills/pdf/SKILL.md", "https://gitlab.com/g/r", "v1.2", "skills/pdf"},
		{"https://gitea.corp/g/r/src/branch/dev/skills", "https://gitea.corp/g/r", "dev", "skills"},
		{"http://10.0.0.5/g/r.git", "http://10.0.0.5/g/r.git", "", ""},
		{"git@gitlab.corp.example:team/sub/skills.git", "git@gitlab.corp.example:team/sub/skills.git", "", ""},
		{"git@gitlab:team/skills.git", "git@gitlab:team/skills.git", "", ""},
		{"ssh://git@gitlab.corp:2222/team/skills.git", "ssh://git@gitlab.corp:2222/team/skills.git", "", ""},
		{"file:///srv/git/skills.git", "file:///srv/git/skills.git", "", ""},
		{"gitlab.com/group/skills", "https://gitlab.com/group/skills", "", ""},
	} {
		src, ok, err := gitSource(c.in)
		if !ok || err != nil {
			t.Errorf("%s: ok %v, %v", c.in, ok, err)
			continue
		}
		if src.Kind != "git" || src.Repo != c.repo || src.Ref != c.ref || src.Path != c.path {
			t.Errorf("%s: read as %+v", c.in, src)
		}
		// what the page shows is read back as the same source
		if back, _, err := gitSource(src.String()); err != nil || !sameGitRepo(back.Repo, src.Repo) || back.Ref != src.Ref || back.Path != src.Path {
			t.Errorf("%s: shown as %s, read back as %+v (%v)", c.in, src.String(), back, err)
		}
	}
	for _, in := range []string{"owner/repo", "C:/Users/me/skills", "hello", "", "skills folder"} {
		if _, ok, _ := gitSource(in); ok {
			t.Errorf("%q read as a git repository", in)
		}
	}
	for _, in := range []string{
		"-uhack", "--upload-pack=touch /tmp/x", "ext::sh -c touch% /tmp/x", "file::/tmp/x", "fd::3",
		"https://me:glpat-secret@gitlab.com/g/r.git", "ftp://host/g/r", "https://gitlab.com/",
		"https://gitlab.com/g/r/-/tree/--upload-pack=x/skills", "https://gitlab.com/g/r/-/tree/main/../../x",
		"git@-oProxyCommand=x:g/r",
	} {
		if _, ok, err := gitSource(in); ok && err == nil {
			t.Errorf("%q isn't refused", in)
		}
	}
}

// realGit is the machine's git, found before any test's sandbox takes
// PATH away: looked up after it, every test of a git repository skipped.
var realGit, _ = exec.LookPath("git")

// gitRemoved, as a file's body in a commit, removes that file or folder.
const gitRemoved = "\x00removed"

// gitRepo makes a bare repository with skills in it, and gives its
// file:// address and a function to commit files to it. git runs with no
// system or user config of the machine's (no credential helper, no
// Keychain).
func gitRepo(t *testing.T, files map[string]string) (string, func(map[string]string)) {
	t.Helper()
	git := realGit
	if git == "" {
		t.Skip("git isn't installed")
	}
	old := gitPath
	gitPath = func() string { return git }
	t.Cleanup(func() { gitPath = old })
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	root := t.TempDir()
	bare, work := filepath.Join(root, "skills.git"), filepath.Join(root, "work")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init", "-q", "--bare", bare)
	run(root, "init", "-q", work)
	commit := func(files map[string]string) {
		for p, body := range files {
			if body == gitRemoved {
				if err := os.RemoveAll(filepath.Join(work, filepath.FromSlash(p))); err != nil {
					t.Fatal(err)
				}
				continue
			}
			write(t, filepath.Join(work, filepath.FromSlash(p)), body)
		}
		run(work, "add", "-A")
		run(work, "commit", "-q", "-m", "skills")
		run(work, "push", "-q", bare, "HEAD:refs/heads/main")
	}
	commit(files)
	p := filepath.ToSlash(bare)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/… on Windows
	}
	return "file://" + p, commit
}

func TestInstallFromGitRepo(t *testing.T) {
	sandbox(t)
	addr, commit := gitRepo(t, map[string]string{
		"skills/pdf/SKILL.md":    "---\nname: pdf\ndescription: PDFs\n---\nv1\n",
		"skills/pdf/notes.md":    "one",
		"skills/review/SKILL.md": "---\nname: review\ndescription: Reviews\n---\n",
		"README.md":              "hi",
	})
	p, err := ProbeSkills(addr)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "git" || len(p.Candidates) != 2 || p.Candidates[0].Path != "skills/pdf" {
		t.Fatalf("probe %+v", p)
	}
	if _, err := InstallSkills(addr, []string{"skills/pdf"}, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	l, _ := load()
	s := l.skill("pdf")
	if s == nil || s.Source.Kind != "git" || s.Source.Repo != addr || s.Source.Path != "skills/pdf" || s.Hash == "" || len(s.Commit) != 40 {
		t.Fatalf("installed as %+v %+v", s, s.Source)
	}
	if b, err := os.ReadFile(filepath.Join(skillDir("pdf"), "notes.md")); err != nil || string(b) != "one" {
		t.Fatalf("notes.md: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(skillDir("pdf"), ".git")); err == nil {
		t.Fatal("the skill has git's folder")
	}
	installedAt := s.Commit

	// the repository changes the skill: a check says so, an update takes it
	commit(map[string]string{"skills/pdf/notes.md": "two"})
	checks, err := CheckSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].Status != "update" {
		t.Fatalf("check %+v", checks)
	}
	if _, err := UpdateSkill("pdf", false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(skillDir("pdf"), "notes.md")); string(b) != "two" {
		t.Fatalf("not updated: %q", b)
	}
	l, _ = load()
	updatedAt := l.skill("pdf").Commit
	if len(updatedAt) != 40 || updatedAt == installedAt {
		t.Fatalf("commit after the update %q (was %q)", updatedAt, installedAt)
	}
	if checks, _ := CheckSkills(); len(checks) != 1 || checks[0].Status != "current" {
		t.Fatalf("after the update %+v", checks)
	}

	// a commit elsewhere in the repository leaves the skill current, at it
	commit(map[string]string{"README.md": "changed"})
	if checks, _ := CheckSkills(); len(checks) != 1 || checks[0].Status != "current" || checks[0].Error != "" {
		t.Fatalf("after a commit elsewhere %+v", checks)
	}
	l, _ = load()
	if c := l.skill("pdf").Commit; c == updatedAt || len(c) != 40 {
		t.Fatalf("the commit found the same isn't kept: %q", c)
	}

	// installing it again is had already
	res, err := InstallSkills(addr, []string{"skills/pdf"}, nil)
	if err != nil || len(res.Had) != 1 {
		t.Fatalf("again: %+v %v", res, err)
	}

	// changed here, an update is refused, naming the repository
	write(t, filepath.Join(skillDir("pdf"), "notes.md"), "mine")
	commit(map[string]string{"skills/pdf/notes.md": "three"})
	_, err = UpdateSkill("pdf", false)
	var edited *EditedError
	if !errors.As(err, &edited) || !strings.Contains(err.Error(), "fetched from "+addr) {
		t.Fatalf("an update over a change here: %v", err)
	}
}

// A GitLab page's address of a folder on a branch installs that folder
// from that branch.
func TestGitTreeURLInstallsFolder(t *testing.T) {
	sandbox(t)
	addr, commit := gitRepo(t, map[string]string{"skills/a/SKILL.md": "---\nname: a\ndescription: on main\n---\n"})
	_ = commit
	p, err := ProbeSkills(addr + "/-/tree/main/skills/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Candidates) != 1 || p.Candidates[0].Name != "a" || p.Candidates[0].Path != "skills/a" {
		t.Fatalf("candidates %+v", p.Candidates)
	}
	if _, err := ProbeSkills(addr + "/-/tree/nope/skills/a"); err == nil || !strings.Contains(err.Error(), "no branch or tag nope") {
		t.Fatalf("a missing branch: %v", err)
	}
	if _, err := ProbeSkills(addr + "/-/tree/main/skills/zz"); err == nil || !strings.Contains(err.Error(), "has no folder skills/zz") {
		t.Fatalf("a missing folder: %v", err)
	}
}

// Without git, the probe says git is needed; nothing else is tried.
func TestGitMissingSaysSo(t *testing.T) {
	sandbox(t)
	old := gitPath
	gitPath = func() string { return "" }
	t.Cleanup(func() { gitPath = old })
	_, err := ProbeSkills("https://gitlab.example.com/team/skills.git")
	if err == nil || !strings.Contains(err.Error(), "git isn't installed") {
		t.Fatalf("got %v", err)
	}
}

// A host that wants a sign-in git can't give it: the error says git
// couldn't sign in, and how the user's own git signs in, never waits for
// a password.
func TestGitSignInRefusedSaysSo(t *testing.T) {
	sandbox(t)
	gitRepo(t, map[string]string{"a/SKILL.md": "---\nname: a\n---\n"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="GitLab"`)
		w.WriteHeader(401)
	}))
	t.Cleanup(srv.Close)
	_, err := ProbeSkills(srv.URL + "/team/private-skills.git")
	if err == nil || !strings.Contains(err.Error(), "git couldn't sign in to 127.0.0.1") {
		t.Fatalf("got %v", err)
	}
}

// What git would read as an option, or a transport that runs a command,
// never reaches git.
func TestGitRefusesOptionsAndTransports(t *testing.T) {
	sandbox(t)
	old := gitPath
	gitPath = func() string { t.Error("git ran"); return "" }
	t.Cleanup(func() { gitPath = old })
	for in, want := range map[string]string{
		"-uhack":                     "can't start with -",
		"--upload-pack=touch x":      "can't start with -",
		"ext::sh -c touch% x":        "transport (ext::)",
		"file::/tmp/x":               "transport (file::)",
		"https://u:tok@gitlab.com/g": "leave the password or token out",
	} {
		if _, err := ProbeSkills(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

// git's own words, as it prints them, read as what they mean.
func TestGitErrorsSaid(t *testing.T) {
	src := Source{Kind: "git", Repo: "https://git.corp.example/team/skills.git", Ref: "dev"}
	for msg, want := range map[string]string{
		"fatal: could not read Username for 'https://git.corp.example': terminal prompts disabled\n":                        "git couldn't sign in to git.corp.example",
		"remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://git.corp.example/team/skills.git/'\n": "git couldn't sign in",
		"git@git.corp.example: Permission denied (publickey).\nfatal: Could not read from remote repository.\n":             "git couldn't sign in",
		"Host key verification failed.\nfatal: Could not read from remote repository.\n":                                    "host key",
		"fatal: couldn't find remote ref dev\n":                                                                             "has no branch or tag dev",
		"fatal: unable to access 'https://git.corp.example/team/skills.git/': Could not resolve host: git.corp.example\n":   "couldn't reach git.corp.example",
		"xcrun: error: invalid active developer path (/Library/Developer/CommandLineTools), missing xcrun\n":                "git isn't installed",
	} {
		if err := gitError(src, msg); !strings.Contains(err.Error(), want) {
			t.Errorf("%q said as %q", msg, err)
		}
	}
}
