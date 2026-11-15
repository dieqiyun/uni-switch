package library

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// fakeDepsTarball serves a repository whose skills/eda asks for its
// dependencies to be installed (easyeda-api-skill: package.json and
// package-lock.json beside SKILL.md), saying version.
func fakeDepsTarball(t *testing.T, version *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball(t, map[string]string{
			"skills/eda/SKILL.md":          "---\nname: eda\ndescription: EDA " + *version + "\n---\nRun npm install first.\n",
			"skills/eda/package.json":      `{"name":"eda","version":"` + *version + `"}`,
			"skills/eda/package-lock.json": `{"lockfileVersion":3,"v":"` + *version + `"}`,
			"skills/eda/run.py":            "print('" + *version + "')\n",
		}))
	}))
	t.Cleanup(srv.Close)
	old := tarballURL
	tarballURL = func(repo, ref string) string { return srv.URL + "/" + repo + "/" + ref }
	t.Cleanup(func() { tarballURL = old })
}

// installDeps writes in dir what npm install and running the skill's
// Python do: node_modules with a link in .bin, a virtualenv, bytecode, and
// the lock file written again.
func installDeps(t *testing.T, dir string) {
	t.Helper()
	write(t, filepath.Join(dir, "node_modules/left-pad/index.js"), "module.exports = 1")
	bin := filepath.Join(dir, "node_modules/.bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(bin, "left-pad"))
	if err := os.Symlink("../left-pad/index.js", filepath.Join(bin, "left-pad")); err != nil {
		t.Skip(err)
	}
	write(t, filepath.Join(dir, ".venv/bin/python"), "python")
	write(t, filepath.Join(dir, "__pycache__/run.cpython-312.pyc"), "bytecode")
	write(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion":3,"packages":{"node_modules/left-pad":{}}}`)
}

// ggbr on Discord: a skill whose SKILL.md says to install its
// dependencies was said to be changed here once they were, so an update
// of every skill left it out and its own asked to replace "your changes".
// What the install writes is no change of the user's: the skill isn't
// said to be changed, updates with the rest, and its dependencies go on
// with the version fetched. A file the user did change still counts.
func TestInstalledDependenciesAreNoChange(t *testing.T) {
	sandbox(t)
	version := "one"
	fakeDepsTarball(t, &version)
	ok(t)(InstallSkills("owner/eda", []string{"skills/eda"}, []string{"claude"}))
	dir := skillDir("eda")
	installDeps(t, dir)
	if s := skillView(t, "eda"); s.Edited {
		t.Fatal("installing eda's dependencies made it changed here")
	}

	version = "two"
	res, err := UpdateSkills()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Updated, []string{"eda"}) || len(res.Unupdated) != 0 {
		t.Fatalf("updated %v, unupdated %+v", res.Updated, res.Unupdated)
	}
	if s := read(t, filepath.Join(dir, "run.py")); s != "print('two')\n" {
		t.Fatalf("run.py after the update: %q", s)
	}
	// the new version's lock file is its own; what was installed stays
	if s := read(t, filepath.Join(dir, "package-lock.json")); s != `{"lockfileVersion":3,"v":"two"}` {
		t.Fatalf("package-lock.json after the update: %q", s)
	}
	for _, p := range []string{"node_modules/left-pad/index.js", ".venv/bin/python", "__pycache__/run.cpython-312.pyc"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("%s didn't go on with the update: %v", p, err)
		}
	}
	if to, err := os.Readlink(filepath.Join(dir, "node_modules/.bin/left-pad")); err != nil || filepath.ToSlash(to) != "../left-pad/index.js" {
		t.Errorf("node_modules/.bin/left-pad: %q, %v", to, err)
	}
	if got, _ := filepath.Glob(filepath.Join(BackupDir(), "*", "library", "skills", "eda")); len(got) != 0 {
		t.Errorf("eda, only installed, was kept with the backups: %v", got)
	}
	if s := skillView(t, "eda"); s.Edited || s.Description != "EDA two" {
		t.Fatalf("eda after the update: %+v", s)
	}

	// the install again, over the new version's lock file, then an edit
	installDeps(t, dir)
	if s := skillView(t, "eda"); s.Edited {
		t.Fatal("installing again after the update made eda changed here")
	}
	write(t, filepath.Join(dir, "run.py"), "print('mine')\n")
	if s := skillView(t, "eda"); !s.Edited {
		t.Fatal("eda's run.py edited here isn't said to be")
	}
	version = "three"
	res, err = UpdateSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 0 || len(res.Unupdated) != 1 {
		t.Fatalf("updated %v, unupdated %+v", res.Updated, res.Unupdated)
	}
	if s := read(t, filepath.Join(dir, "run.py")); s != "print('mine')\n" {
		t.Fatalf("updating every skill wrote over the edit: %q", s)
	}
}

// A skill fetched before uni-switch kept Own has only Hash, of the files
// fetched with their lock file: installed folders and bytecode are no
// change, and nor is the lock file left as it came. One the install
// wrote again can't be told from an edit until the skill is updated.
func TestInstalledDependenciesBeforeOwnWasKept(t *testing.T) {
	sandbox(t)
	version := "one"
	fakeDepsTarball(t, &version)
	ok(t)(InstallSkills("owner/eda", []string{"skills/eda"}, []string{"claude"}))
	mu.Lock()
	l, err := load()
	if err == nil {
		l.skill("eda").Own = ""
		err = l.save()
	}
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	dir := skillDir("eda")
	lock := read(t, filepath.Join(dir, "package-lock.json"))
	installDeps(t, dir)
	write(t, filepath.Join(dir, "package-lock.json"), lock)
	if s := skillView(t, "eda"); s.Edited {
		t.Fatal("eda's installed folders made it changed here")
	}
	write(t, filepath.Join(dir, "package-lock.json"), lock+" ")
	if s := skillView(t, "eda"); !s.Edited {
		t.Fatal("with no Own, a lock file written again is no longer told as changed")
	}
}

// An agent given its skills as copies runs the install in its copy: that
// copy isn't changed by the user either.
func TestInstalledDependenciesInACopy(t *testing.T) {
	h := sandbox(t)
	version := "one"
	fakeDepsTarball(t, &version)
	ok(t)(InstallSkills("owner/eda", []string{"skills/eda"}, []string{"claude"}))
	ok(t)(SetSkillHow("", HowCopy))
	p := filepath.Join(h, ".claude/skills/eda")
	if linked(p) {
		t.Fatal("claude's eda is still a link")
	}
	installDeps(t, p)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(filepath.Join(p, "node_modules/left-pad/index.js"), later, later)
	if s := skillView(t, "eda"); s.Edited {
		t.Fatal("installing in Claude Code's copy made eda changed here")
	}
	write(t, filepath.Join(p, "run.py"), "print('mine')\n")
	os.Chtimes(filepath.Join(p, "run.py"), later, later)
	if s := skillView(t, "eda"); !s.Edited {
		t.Fatal("run.py edited in Claude Code's copy isn't said to be")
	}
}
