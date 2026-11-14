package library

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func repoIn(t *testing.T) map[string]string {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, s := range v.Skills {
		out[s.Name] = s.Repo
	}
	return out
}

// npx skills add with $XDG_STATE_HOME set (@_yukirock on X): the skills
// CLI keeps its lock at $XDG_STATE_HOME/skills/.skill-lock.json and the
// skills in ~/.agents/skills, and each is grouped by the repository that
// lock names. One the older ~/.agents lock alone names is grouped too.
func TestSkillRepoFromXDGLock(t *testing.T) {
	h := sandbox(t)
	state := filepath.Join(h, "state")
	t.Setenv("XDG_STATE_HOME", state)
	for _, n := range []string{"grill-me", "tdd", "old"} {
		skill(t, filepath.Join(h, ".agents/skills", n), n, n)
		os.MkdirAll(filepath.Join(h, ".claude/skills"), 0o755)
		dirLink(filepath.Join(h, ".agents/skills", n), filepath.Join(h, ".claude/skills", n))
	}
	write(t, filepath.Join(state, "skills/.skill-lock.json"), `{"version":3,"skills":{
		"grill-me":{"source":"mattpocock/skills","sourceType":"github","sourceUrl":"https://github.com/mattpocock/skills.git"},
		"tdd":{"source":"obra/superpowers","sourceType":"github","sourceUrl":"https://github.com/obra/superpowers.git"}}}`)
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{
		"tdd":{"source":"stale/repo","sourceType":"github"},
		"old":{"source":"me/old-skills","sourceType":"github"}}}`)
	ok(t)(ImportSkills([]string{"grill-me", "tdd", "old"}))
	got := repoIn(t)
	for n, r := range map[string]string{"grill-me": "mattpocock/skills", "tdd": "obra/superpowers", "old": "me/old-skills"} {
		if got[n] != r {
			t.Errorf("%s: repo %q, want %q", n, got[n], r)
		}
	}
}

// checkout makes dir a git checkout whose config is cfg, with what git
// looks for in its .git (provider.GitAt) as git init lays it out.
func checkout(t *testing.T, dir, cfg string) {
	t.Helper()
	write(t, filepath.Join(dir, ".git/HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(dir, ".git/config"), cfg)
	for _, d := range []string{"objects", "refs/heads"} {
		if err := os.MkdirAll(filepath.Join(dir, ".git", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// A skill not installed from GitHub is grouped with the repository it
// came from all the same (White Immortal on Discord): the skills CLI's
// lock names it, or the git checkout it's in, whatever the remote's form,
// and never with a user or token the remote had. The checkout is the one
// git finds, past an empty .git on the way (#3675). A skill in ~/.claude
// kept in git (the user's dotfiles) is not from that repository.
func TestSkillRepoTraced(t *testing.T) {
	h := sandbox(t)
	// npx skills add mattpocock/skills: in ~/.agents/skills, in its lock
	skill(t, filepath.Join(h, ".agents/skills/grill-me"), "grill-me", "Grill")
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{
		"grill-me":{"source":"mattpocock/skills","sourceType":"github","sourceUrl":"https://github.com/mattpocock/skills.git","skillPath":"skills/grill-me/SKILL.md"},
		"tdd":{"source":"x","sourceType":"git","sourceUrl":"git@gitlab.com:team/skills.git"}}}`)
	os.MkdirAll(filepath.Join(h, ".claude/skills"), 0o755)
	dirLink(filepath.Join(h, ".agents/skills/grill-me"), filepath.Join(h, ".claude/skills/grill-me"))
	// a clone of the repository, linked from the library
	clone := filepath.Join(h, "code/skills")
	skill(t, filepath.Join(clone, "skills/write-a-prd"), "write-a-prd", "PRD")
	checkout(t, clone, "[core]\n\tbare = false\n[remote \"upstream\"]\n\turl = https://github.com/other/fork.git\n[remote \"origin\"]\r\n\turl = https://me:ghp_secret@github.com/mattpocock/skills.git\r\n")
	// an empty .git between the skill and its clone, which git passes over
	if err := os.MkdirAll(filepath.Join(clone, "skills/.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a skill that is a clone of its own, moved in
	skill(t, filepath.Join(h, ".claude/skills/review"), "review", "Review")
	checkout(t, filepath.Join(h, ".claude/skills/review"), "[remote \"origin\"]\n\turl = git@gitlab.com:me/review.git\n")
	// ~/.claude in git: the user's dotfiles
	checkout(t, filepath.Join(h, ".claude"), "[remote \"origin\"]\n\turl = git@github.com:me/dotfiles.git\n")
	skill(t, filepath.Join(h, ".claude/skills/notes"), "notes", "Mine")

	ok(t)(ImportSkills([]string{"grill-me", "review", "notes"}))
	p, err := ProbeSkills(filepath.Join(clone, "skills/write-a-prd"))
	if err != nil || len(p.Candidates) != 1 {
		t.Fatalf("probe: %+v %v", p, err)
	}
	ok(t)(InstallSkills(filepath.Join(clone, "skills/write-a-prd"), []string{p.Candidates[0].Path}, nil))

	got := repoIn(t)
	want := map[string]string{"grill-me": "mattpocock/skills", "write-a-prd": "mattpocock/skills", "review": "gitlab.com/me/review", "notes": ""}
	for n, r := range want {
		if got[n] != r {
			t.Errorf("%s: repo %q, want %q", n, got[n], r)
		}
	}
	b, _ := os.ReadFile(path())
	if strings.Contains(string(b), "ghp_secret") {
		t.Error("the remote's token was written down")
	}
	// the clone moved in kept where it came from
	if s := mustLoad(t).skill("review"); s.From != "gitlab.com/me/review" {
		t.Errorf("review's From: %q", s.From)
	}
}

// A site's kit of skills (npx skills add from a /.well-known/agent-skills/
// index) is one group, by the lock's sourceBaseUrl (#1862, WardLu): the
// reporter's three Feishu skills and two Fish Audio ones, each with a
// sourceUrl of its own, were five groups. Another kit under the same host
// with another base URL stays apart. A lock with no sourceBaseUrl is
// grouped by the base in its sourceUrl, before /.well-known/.
func TestWellKnownKitGrouped(t *testing.T) {
	h := sandbox(t)
	const feishu = "https://open.feishu.cn/lark-cli/skills/regular"
	lock := map[string][2]string{ // name: sourceBaseUrl, sourceUrl
		"lark-base":      {feishu, feishu + "/.well-known/agent-skills/lark-base.tar.gz"},
		"lark-doc":       {feishu, feishu + "/.well-known/agent-skills/lark-doc.tar.gz"},
		"lark-sheets":    {feishu, feishu + "/.well-known/agent-skills/lark-sheets.tar.gz"},
		"fish-audio-api": {"https://docs.fish.audio", "https://docs.fish.audio/.well-known/agent-skills/fish-audio-api/skill.md"},
		"fish-audio-sdk": {"https://docs.fish.audio/", "https://docs.fish.audio/.well-known/agent-skills/fish-audio-sdk/skill.md"},
		// the same host, another kit
		"lark-admin": {"https://open.feishu.cn/lark-cli/skills/admin", "https://open.feishu.cn/lark-cli/skills/admin/.well-known/agent-skills/lark-admin.tar.gz"},
		// a lock from before sourceBaseUrl
		"lark-old": {"", feishu + "/.well-known/agent-skills/lark-old.tar.gz"},
		// a kit one folder down a site, which reads like owner/repo
		"kit-one": {"https://docs.example.com/kit", "https://docs.example.com/kit/.well-known/agent-skills/kit-one/SKILL.md"},
	}
	var entries []string
	os.MkdirAll(filepath.Join(h, ".claude/skills"), 0o755)
	var names []string
	for n, u := range lock {
		skill(t, filepath.Join(h, ".agents/skills", n), n, "D")
		dirLink(filepath.Join(h, ".agents/skills", n), filepath.Join(h, ".claude/skills", n))
		base := ""
		if u[0] != "" {
			base = `,"sourceBaseUrl":"` + u[0] + `"`
		}
		entries = append(entries, `"`+n+`":{"sourceType":"well-known","sourceUrl":"`+u[1]+`"`+base+`}`)
		names = append(names, n)
	}
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{`+strings.Join(entries, ",")+`}}`)
	ok(t)(ImportSkills(names))

	got := repoIn(t)
	for n, want := range map[string]string{
		"lark-base": "open.feishu.cn/lark-cli/skills/regular", "lark-doc": "open.feishu.cn/lark-cli/skills/regular",
		"lark-sheets": "open.feishu.cn/lark-cli/skills/regular", "lark-old": "open.feishu.cn/lark-cli/skills/regular",
		"fish-audio-api": "docs.fish.audio", "fish-audio-sdk": "docs.fish.audio",
		"lark-admin": "open.feishu.cn/lark-cli/skills/admin", "kit-one": "docs.example.com/kit",
	} {
		if got[n] != want {
			t.Errorf("%s: repo %q, want %q", n, got[n], want)
		}
	}

	// Group again fetches GitHub repositories only: none of these is one
	var asked []string
	oldT := tarballURL
	tarballURL = func(repo, ref string) string { asked = append(asked, repo); return "http://127.0.0.1:1/" }
	t.Cleanup(func() { tarballURL = oldT })
	if _, err := RegroupSkills(); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 0 {
		t.Errorf("fetched from GitHub: %v", asked)
	}
}

// #1862 (WardLu, v0.1.1223): skills brought into the library before
// well-known kits were grouped by their base kept the lock's key of then
// as their From, each skill's own sourceUrl, and From is read before the
// lock, so Feishu's three were three groups and Fish Audio's two were two
// after the fix. The reporter's lock entries, with From as v0.1.1217's
// ImportSkills wrote it.
func TestWellKnownKitFromBeforeGrouped(t *testing.T) {
	h := sandbox(t)
	lock := `{"skills":{
  "lark-base":{"source":"open.feishu.cn","sourceType":"well-known","sourceUrl":"https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/lark-base.tar.gz","sourceBaseUrl":"https://open.feishu.cn/lark-cli/skills/regular"},
  "lark-doc":{"source":"open.feishu.cn","sourceType":"well-known","sourceUrl":"https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/lark-doc.tar.gz","sourceBaseUrl":"https://open.feishu.cn/lark-cli/skills/regular"},
  "lark-sheets":{"source":"open.feishu.cn","sourceType":"well-known","sourceUrl":"https://open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/lark-sheets.tar.gz","sourceBaseUrl":"https://open.feishu.cn/lark-cli/skills/regular"},
  "fish-audio-api":{"source":"docs.fish.audio","sourceType":"well-known","sourceUrl":"https://docs.fish.audio/.well-known/agent-skills/fish-audio-api/skill.md","sourceBaseUrl":"https://docs.fish.audio"},
  "fish-audio-sdk":{"source":"docs.fish.audio","sourceType":"well-known","sourceUrl":"https://docs.fish.audio/.well-known/agent-skills/fish-audio-sdk/skill.md","sourceBaseUrl":"https://docs.fish.audio"}
}}`
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), lock)
	before := map[string]string{ // remoteRepo of each sourceUrl, the key before 97dfed0f3
		"lark-base":      "open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/lark-base.tar.gz",
		"lark-doc":       "open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/lark-doc.tar.gz",
		"lark-sheets":    "open.feishu.cn/lark-cli/skills/regular/.well-known/agent-skills/lark-sheets.tar.gz",
		"fish-audio-api": "docs.fish.audio/.well-known/agent-skills/fish-audio-api/skill.md",
		"fish-audio-sdk": "docs.fish.audio/.well-known/agent-skills/fish-audio-sdk/skill.md",
	}
	var names []string
	for n := range before {
		skill(t, filepath.Join(h, ".agents/skills", n), n, "D")
		names = append(names, n)
	}
	ok(t)(ImportSkills(names))
	l := mustLoad(t)
	for n, f := range before {
		l.skill(n).From = f
	}
	if err := l.save(); err != nil {
		t.Fatal(err)
	}

	got := repoIn(t)
	for n := range before {
		want := "docs.fish.audio"
		if strings.HasPrefix(n, "lark-") {
			want = "open.feishu.cn/lark-cli/skills/regular"
		}
		if got[n] != want {
			t.Errorf("%s: repo %q, want %q", n, got[n], want)
		}
	}
}

// A well-known kit's base, from the lock's sourceBaseUrl or else its
// sourceUrl; neither an http(s) address, its sourceUrl as a remote.
func TestWellKnownBase(t *testing.T) {
	for _, c := range []struct{ base, src, want string }{
		{"https://open.feishu.cn/lark-cli/skills/regular", "", "open.feishu.cn/lark-cli/skills/regular"},
		{"https://Docs.Fish.Audio/", "", "docs.fish.audio"},
		{"https://www.example.com/kit/?v=2", "", "example.com/kit"},
		{"https://me:tok@example.com/kit", "", "example.com/kit"},
		{"https://example.com/kit/.well-known/agent-skills/index.json", "", "example.com/kit"},
		{"", "https://docs.fish.audio/.well-known/agent-skills/fish-audio-api/skill.md", "docs.fish.audio"},
		{"not a url", "https://example.com/a/.well-known/agent-skills/x.tar.gz", "example.com/a"},
		{"", "https://example.com/team/kit.git", "example.com/team/kit"},
		{"file:///etc/kit", "", ""},
		{"https://example.com/../etc", "", ""},
	} {
		if got := wellKnownBase(c.base, c.src); got != c.want {
			t.Errorf("%q %q: %q, want %q", c.base, c.src, got, c.want)
		}
	}
}

// The remote forms git takes, each read as the repository.
func TestRemoteRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/mattpocock/skills":          "mattpocock/skills",
		"https://www.github.com/mattpocock/skills.git/": "mattpocock/skills",
		"git@github.com:mattpocock/skills.git":          "mattpocock/skills",
		"ssh://git@github.com/mattpocock/skills.git":    "mattpocock/skills",
		"https://user:tok@github.com/a/b/tree/main/x":   "a/b",
		"git@gitlab.com:group/sub/proj.git":             "gitlab.com/group/sub/proj",
		"https://tok@codeberg.org/me/s.git":             "codeberg.org/me/s",
		"mattpocock/skills":                             "mattpocock/skills",
		"/Users/me/skills":                              "",
		"https://github.com/onlyowner":                  "",
		"https://example.com/../etc":                    "",
	} {
		if got := remoteRepo(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}

// Installing from a repository again groups the skills the library has
// already from it (White Immortal: installed before, from that very
// address): those whose SKILL.md is the repository's own. One of the same
// name that differs is someone else's skill, and stays where it was.
func TestInstallAgainNotesFrom(t *testing.T) {
	h := sandbox(t)
	newFakeGitHub(t)
	// the fake repository's skills/pdf/SKILL.md, as installed by hand
	write(t, filepath.Join(h, ".claude/skills/pdf/SKILL.md"), "---\nname: pdf\ndescription: PDFs\n---\n")
	ok(t)(ImportSkill("pdf"))
	if r := repoIn(t)["pdf"]; r != "" {
		t.Fatalf("pdf has a repo before: %q", r)
	}
	if _, err := ProbeSkills("someone/fromagain"); err != nil {
		t.Fatal(err)
	}
	if r := repoIn(t)["pdf"]; r != "someone/fromagain" {
		t.Errorf("pdf after installing from its repository again: %q", r)
	}

	// another's pdf, not the same file: not noted
	h = sandbox(t)
	write(t, filepath.Join(h, ".claude/skills/pdf/SKILL.md"), "---\nname: pdf\ndescription: My own\n---\n")
	ok(t)(ImportSkill("pdf"))
	if _, err := ProbeSkills("someone/notmine"); err != nil {
		t.Fatal(err)
	}
	if r := repoIn(t)["pdf"]; r != "" {
		t.Errorf("another's pdf grouped with the repository: %q", r)
	}
}

func mustLoad(t *testing.T) *Library {
	t.Helper()
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A pack added a while ago is mostly older copies of its skills (White
// Immortal on Discord: 20 of mattpocock/skills' 38 grouped, the rest
// edited there since). Group again fetches each repository the library
// has skills from, and the skills it has by name that are traced to
// nowhere join the rest of it; one of the user's own it doesn't have, and
// one traced to another repository, stay where they are.
func TestRegroupOlderCopies(t *testing.T) {
	h := sandbox(t)
	repos := map[string]map[string]string{
		"someone/pack": {
			"skills/eng/same/SKILL.md":    "---\nname: same\ndescription: Same\n---\nBody\n",
			"skills/eng/edited/SKILL.md":  "---\nname: edited\ndescription: Edited since\n---\nNew body\n",
			"skills/misc/later/SKILL.md":  "---\nname: later\ndescription: Rewritten\n---\nAll new\n",
			"skills/misc/locked/SKILL.md": "---\nname: locked\ndescription: Theirs\n---\n",
		},
		"other/locked": {"skills/locked/SKILL.md": "---\nname: locked\ndescription: Locked\n---\n"},
	}
	var asked []string
	var amu sync.Mutex
	tar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimPrefix(r.URL.Path, "/tar/")
		amu.Lock()
		asked = append(asked, repo)
		amu.Unlock()
		files, ok := repos[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(tarball(t, files))
	}))
	t.Cleanup(tar.Close)
	oldT := tarballURL
	tarballURL = func(repo, ref string) string { return tar.URL + "/tar/" + repo }
	t.Cleanup(func() { tarballURL = oldT })

	// the copies as added before: same is the repository's own, edited
	// and later older ones, mine the user's own
	write(t, filepath.Join(h, ".claude/skills/same/SKILL.md"), repos["someone/pack"]["skills/eng/same/SKILL.md"])
	skill(t, filepath.Join(h, ".claude/skills/edited"), "edited", "Edited")
	skill(t, filepath.Join(h, ".claude/skills/later"), "later", "Later")
	skill(t, filepath.Join(h, ".claude/skills/mine"), "mine", "Mine")
	// npx skills add other/locked: traced by the lock
	skill(t, filepath.Join(h, ".agents/skills/locked"), "locked", "Locked")
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{"locked":{"source":"other/locked","sourceType":"github"}}}`)
	dirLink(filepath.Join(h, ".agents/skills/locked"), filepath.Join(h, ".claude/skills/locked"))
	ok(t)(ImportSkills([]string{"same", "edited", "later", "mine", "locked"}))
	// same grouped before, as installing from the repository again did
	l := mustLoad(t)
	l.skill("same").From = "someone/pack"
	if err := l.save(); err != nil {
		t.Fatal(err)
	}
	if r := repoIn(t)["edited"]; r != "" {
		t.Fatalf("edited has a repo before: %q", r)
	}

	n, err := RegroupSkills()
	if err != nil {
		t.Fatal(err)
	}
	got := repoIn(t)
	want := map[string]string{"same": "someone/pack", "edited": "someone/pack", "later": "someone/pack", "mine": "", "locked": "other/locked"}
	for name, r := range want {
		if got[name] != r {
			t.Errorf("%s: repo %q, want %q", name, got[name], r)
		}
	}
	if n != 2 {
		t.Errorf("grouped %d, want 2", n)
	}
	amu.Lock()
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"other/locked", "someone/pack"}) {
		t.Errorf("fetched %v", asked)
	}
	asked = nil
	amu.Unlock()

	// nothing more to group: none grouped, nothing wrong
	if n, err := RegroupSkills(); err != nil || n != 0 {
		t.Errorf("again: %d %v", n, err)
	}
	// GitHub not answering: said, nothing changed
	tarballURL = func(repo, ref string) string { return tar.URL + "/tar/gone/" + repo }
	if _, err := RegroupSkills(); err == nil {
		t.Error("no repository fetched, and no error")
	}
	if r := repoIn(t)["edited"]; r != "someone/pack" {
		t.Errorf("edited after a failed regroup: %q", r)
	}
}

// A copy in the library's own folder that is a repository's skill to the
// file is bound to it when that repository is pasted again, and updates
// from there as one installed from it does (RockyShowlle on X: installed
// by hand before, now to sync with upstream). One with a file of the
// user's own beside it, and one linked to the skills CLI's folder, are
// grouped only: the user's file is never written over, and the skills CLI
// keeps what it put in ~/.agents.
func TestPastedRepoBindsSameCopy(t *testing.T) {
	h := sandbox(t)
	var rmu sync.Mutex
	repo := map[string]string{
		"skills/same/SKILL.md":   "---\nname: same\ndescription: Same\n---\nBody\n",
		"skills/same/ref.md":     "notes\n",
		"skills/extra/SKILL.md":  "---\nname: extra\ndescription: Extra\n---\n",
		"skills/shared/SKILL.md": "---\nname: shared\ndescription: Shared\n---\n",
	}
	tar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rmu.Lock()
		defer rmu.Unlock()
		w.Write(tarball(t, repo))
	}))
	t.Cleanup(tar.Close)
	oldT := tarballURL
	tarballURL = func(repo, ref string) string { return tar.URL + "/tar/" + repo }
	t.Cleanup(func() { tarballURL = oldT })

	write(t, filepath.Join(h, ".claude/skills/same/SKILL.md"), repo["skills/same/SKILL.md"])
	write(t, filepath.Join(h, ".claude/skills/same/ref.md"), repo["skills/same/ref.md"])
	write(t, filepath.Join(h, ".claude/skills/extra/SKILL.md"), repo["skills/extra/SKILL.md"])
	write(t, filepath.Join(h, ".claude/skills/extra/mine.md"), "my own notes\n")
	write(t, filepath.Join(h, ".agents/skills/shared/SKILL.md"), repo["skills/shared/SKILL.md"])
	dirLink(filepath.Join(h, ".agents/skills/shared"), filepath.Join(h, ".claude/skills/shared"))
	ok(t)(ImportSkills([]string{"same", "extra", "shared"}))

	if _, err := ProbeSkills("someone/upstream"); err != nil {
		t.Fatal(err)
	}
	l := mustLoad(t)
	if s := l.skill("same"); s.Source == nil || s.Source.Kind != "github" || s.Source.Repo != "someone/upstream" || s.Source.Path != "skills/same" || s.Hash == "" || s.From != "" {
		t.Fatalf("same, the repository's to the file: source %+v hash %q from %q", s.Source, s.Hash, s.From)
	}
	if s := l.skill("extra"); s.Source != nil || s.From != "someone/upstream" {
		t.Errorf("extra, with a file of the user's: source %+v from %q", s.Source, s.From)
	}
	if s := l.skill("shared"); s.Source == nil || s.Source.Kind != "folder" || s.From != "someone/upstream" {
		t.Errorf("shared, the skills CLI's: source %+v from %q", s.Source, s.From)
	}
	if got := repoIn(t); got["same"] != "someone/upstream" || got["extra"] != "someone/upstream" || got["shared"] != "someone/upstream" {
		t.Errorf("grouped: %v", got)
	}

	// upstream changes: the bound copy updates from there
	rmu.Lock()
	repo["skills/same/SKILL.md"] = "---\nname: same\ndescription: Same\n---\nNew body\n"
	rmu.Unlock()
	ok(t)(UpdateSkill("same", false))
	if b, _ := os.ReadFile(filepath.Join(skillDir("same"), "SKILL.md")); !strings.Contains(string(b), "New body") {
		t.Errorf("same after an update: %q", b)
	}
	if _, err := UpdateSkill("extra", false); err == nil {
		t.Error("extra updated from GitHub")
	}
	if b, _ := os.ReadFile(filepath.Join(skillDir("extra"), "mine.md")); string(b) != "my own notes\n" {
		t.Errorf("extra's own file: %q", b)
	}
}

// A skill the skills CLI installed (npx skills add) and that was brought
// in and handed over kept only a From label, and update said it "isn't
// from GitHub or a git repository" (#4645, Apri1qing): the lock that
// names where it came from for grouping names it for updating too, and
// one updated that way is the repository's own from then on.
func TestSkillLockOriginUpdates(t *testing.T) {
	h := sandbox(t)
	noGoose(t, h)
	installed := time.Now().Add(-time.Hour).UTC()
	ts := installed.Format(time.RFC3339Nano)
	// npx skills add mattpocock/skills --skill grill-me -g: in
	// ~/.agents/skills, the lock saying where it came from
	skill(t, filepath.Join(h, ".agents/skills/grill-me"), "grill-me", "old")
	writtenAt(t, filepath.Join(h, ".agents/skills/grill-me"), installed)
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{
		"grill-me":{"source":"mattpocock/skills","sourceType":"github","sourceUrl":"https://github.com/mattpocock/skills.git","skillPath":"skills/grill-me/SKILL.md","installedAt":"`+ts+`","updatedAt":"`+ts+`"}}}`)
	var rmu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rmu.Lock()
		defer rmu.Unlock()
		w.Write(tarball(t, map[string]string{
			"skills/grill-me/SKILL.md": "---\nname: grill-me\ndescription: new\n---\n",
		}))
	}))
	defer srv.Close()
	old := tarballURL
	tarballURL = func(repo, ref string) string { return srv.URL + "/" + repo + "/" + ref }
	defer func() { tarballURL = old }()

	ok(t)(ImportSkills([]string{"grill-me"}))
	ok(t)(HandOverSkills([]string{"grill-me"}))
	// handed over, it is the library's own, with only a From label
	if s := mustLoad(t).skill("grill-me"); s.Source != nil {
		t.Fatalf("handed over: source %+v, want none", s.Source)
	}
	// the row says where the lock says it came from
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Skills {
		if s.Name == "grill-me" && s.Origin != "https://github.com/mattpocock/skills" {
			t.Errorf("grill-me's origin: %q, want https://github.com/mattpocock/skills", s.Origin)
		}
	}
	if _, err := UpdateSkill("grill-me", false); err != nil {
		t.Fatalf("update: %v", err)
	}
	s := mustLoad(t).skill("grill-me")
	if s.Source == nil || s.Source.Kind != "github" || s.Source.Repo != "mattpocock/skills" {
		t.Errorf("after: source %+v, want github mattpocock/skills", s.Source)
	}
	if b, _ := os.ReadFile(filepath.Join(skillDir("grill-me"), "SKILL.md")); !strings.Contains(string(b), "new") {
		t.Errorf("grill-me after an update: %q", b)
	}
}

// A lock entry left by a skill of the same name since deleted isn't where
// the library's copy came from (AzheRise on X): a folder the CLI didn't
// write isn't updated from the entry's repository, and one an old lock
// without a time names tells no skill of that name apart (it groups, and
// it stays as it is).
func TestSkillLockOriginStale(t *testing.T) {
	h := sandbox(t)
	noGoose(t, h)
	skill(t, filepath.Join(h, ".agents/skills/grill-me"), "grill-me", "mine, made today")
	skill(t, filepath.Join(h, ".agents/skills/undated"), "undated", "An old lock's")
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{
		"grill-me":{"source":"other/skills","sourceType":"github","updatedAt":"2020-01-01T00:00:00Z"},
		"undated":{"source":"other/undated","sourceType":"github"}}}`)
	ok(t)(ImportSkills([]string{"grill-me", "undated"}))
	if _, err := UpdateSkill("grill-me", false); err == nil || !strings.Contains(err.Error(), "isn't from GitHub or a git repository") {
		t.Errorf("a folder the CLI didn't write: %v, want isn't from GitHub or a git repository", err)
	}
	if _, err := UpdateSkill("undated", false); err == nil || !strings.Contains(err.Error(), "isn't from GitHub or a git repository") {
		t.Errorf("a lock without a time: %v, want isn't from GitHub or a git repository", err)
	}
}

// A skill installed from a folder of the user's own is theirs, whatever a
// lock entry of its name says: an update doesn't turn it into the
// repository's, and its row names no GitHub origin (#4645's lockOrigin,
// held to the grouping's rule: no source, or one in ~/.agents/skills).
func TestSkillLockOriginLeavesUsersFolder(t *testing.T) {
	h := sandbox(t)
	noGoose(t, h)
	installed := time.Now().Add(-time.Hour).UTC()
	ts := installed.Format(time.RFC3339Nano)
	src := filepath.Join(h, "mine/skills")
	skill(t, filepath.Join(src, "grill-me"), "grill-me", "mine")
	ok(t)(InstallSkills(src, []string{"grill-me"}, nil))
	writtenAt(t, realDir(skillDir("grill-me")), installed)
	write(t, filepath.Join(h, ".agents/.skill-lock.json"), `{"version":3,"skills":{
		"grill-me":{"source":"mattpocock/skills","sourceType":"github","installedAt":"`+ts+`","updatedAt":"`+ts+`"}}}`)
	if s := mustLoad(t).skill("grill-me"); s.Source == nil || s.Source.Kind != "folder" {
		t.Fatalf("installed from a folder: source %+v", s.Source)
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Skills {
		if s.Name == "grill-me" && strings.Contains(s.Origin, "github.com") {
			t.Errorf("a folder of the user's: origin %q", s.Origin)
		}
	}
	UpdateSkill("grill-me", false)
	if s := mustLoad(t).skill("grill-me"); s.Source == nil || s.Source.Kind != "folder" {
		t.Errorf("after an update: source %+v, want the user's folder", s.Source)
	}
}
