package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeSkillRepo serves a repository's files: codeload's tarball, the API's
// tree of it (with an ETag, answering 304 to it) and the last commit to
// any folder. asked counts the requests, "tar", "tree" and "tree 304".
func fakeSkillRepo(t *testing.T, files map[string]string) (asked map[string]int, mu *sync.Mutex) {
	t.Helper()
	asked, mu = map[string]int{}, &sync.Mutex{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/tar/"):
			asked["tar"]++
			w.Write(tarball(t, files))
		case strings.Contains(r.URL.Path, "/git/trees/"):
			if r.URL.Query().Get("recursive") != "1" {
				t.Errorf("tree asked without recursive: %s", r.URL)
			}
			// git's ref:path is that folder's own list, as GitHub
			// answers it; no such folder is a 404
			_, under, _ := strings.Cut(r.URL.Path[strings.Index(r.URL.Path, "/git/trees/")+len("/git/trees/"):], ":")
			var tree gitTree
			paths := []string{}
			for p := range files {
				if under == "" {
					paths = append(paths, p)
				} else if rest, in := strings.CutPrefix(p, under+"/"); in {
					paths = append(paths, rest)
				}
			}
			if len(paths) == 0 {
				http.NotFound(w, r)
				return
			}
			sort.Strings(paths)
			for _, p := range paths {
				tree.Tree = append(tree.Tree, gitEntry{Path: p, Type: "blob"})
			}
			b, _ := json.Marshal(tree)
			etag := `"` + hashOf(b) + `"`
			if r.Header.Get("If-None-Match") == etag {
				asked["tree 304"]++
				w.WriteHeader(304)
				return
			}
			asked["tree"]++
			w.Header().Set("ETag", etag)
			w.Write(b)
		default:
			w.Write([]byte(`[{"sha":"s1","commit":{"message":"m","committer":{"date":"2026-09-01T10:00:00Z"}}}]`))
		}
	}))
	t.Cleanup(srv.Close)
	oldT, oldA := tarballURL, githubAPI
	tarballURL = func(repo, ref string) string { return srv.URL + "/tar/" + repo }
	githubAPI = srv.URL
	t.Cleanup(func() { tarballURL, githubAPI = oldT, oldA })
	return asked, mu
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func newNames(t *testing.T) []string {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, n := range v.NewSkills {
		out = append(out, n.Name)
	}
	return out
}

// Raven's report: a repository added skills after some of it was
// installed (dontbesilent2025/dbskill, mattpocock/skills), and a check for
// updates only updated the ones the library had. It offers the new ones
// now, beside the installed ones, to add or set aside.
func TestCheckSkillsFindsNewOnes(t *testing.T) {
	sandbox(t)
	files := map[string]string{
		"README.md":                "# skills",
		"skills/dbs/SKILL.md":      "---\nname: dbs\ndescription: The main one\n---\n",
		"skills/dbs-goal/SKILL.md": "---\nname: dbs-goal\ndescription: Goals\n---\n",
		"skills/dbs-hook/SKILL.md": "---\nname: dbs-hook\ndescription: Hooks\n---\n",
		"skills/dbs-hook/ref/x.md": "x",
		"other/elsewhere/SKILL.md": "---\nname: elsewhere\n---\n",
	}
	asked, amu := fakeSkillRepo(t, files)
	count := func(k string) int { amu.Lock(); defer amu.Unlock(); return asked[k] }

	// two of the three picked; the third was shown and left
	ok(t)(InstallSkills("dontbesilent2025/dbskill", []string{"skills/dbs", "skills/dbs-goal"}, []string{"claude", "codex"}))
	check(t) // learns which commit the installed skills are at
	if got := newNames(t); len(got) != 0 {
		t.Fatalf("offered what the picker showed already: %v", got)
	}
	tars := count("tar")
	check(t)
	if count("tar") != tars {
		t.Errorf("fetched the repository when its tree had nothing new: %v", asked)
	}

	// the repository adds two skills beside them, one inside another skill
	// and one hidden, and one far from them
	files["skills/dbs-save/SKILL.md"] = "---\nname: dbs-save\ndescription: Saves\n---\n"
	files["skills/dbs-xhs-title/SKILL.md"] = "---\nname: dbs-xhs-title\ndescription: Titles\n---\n"
	files["skills/dbs-save/inner/SKILL.md"] = "---\nname: inner\n---\n"
	files["skills/.wip/SKILL.md"] = "---\nname: wip\n---\n"
	check(t)
	if got := newNames(t); !slices.Equal(got, []string{"dbs-save", "dbs-xhs-title"}) {
		t.Fatalf("new: %v", got)
	}
	v, _ := Read(nil)
	n := v.NewSkills[0]
	if n.Repo != "dontbesilent2025/dbskill" || n.Path != "skills/dbs-save" || n.Description != "Saves" ||
		n.ID != "https://github.com/dontbesilent2025/dbskill/tree/HEAD/skills/dbs-save" || !slices.Equal(n.Agents, []string{"claude", "codex"}) {
		t.Errorf("new skill: %+v", n)
	}

	// added from where the page offers it, for the agents that have the
	// others: one of the repository's like them, updated with them
	if n.From != "https://github.com/dontbesilent2025/dbskill/tree/HEAD/skills" {
		t.Errorf("from %q", n.From)
	}
	ok(t)(AddNewSkills([]string{n.ID}))
	l, _ := load()
	if s := l.skill("dbs-save"); s == nil || s.Source.Repo != "dontbesilent2025/dbskill" || s.Source.Ref != "" || s.Source.Path != "skills/dbs-save" {
		t.Fatalf("added: %+v", s)
	}
	if got := newNames(t); !slices.Equal(got, []string{"dbs-xhs-title"}) {
		t.Errorf("after adding one: %v", got)
	}

	// the other set aside: not offered again by the next check
	if err := IgnoreNewSkills([]string{v.NewSkills[1].ID}); err != nil {
		t.Fatal(err)
	}
	if got := newNames(t); len(got) != 0 {
		t.Errorf("after setting one aside: %v", got)
	}
	check(t)
	if got := newNames(t); len(got) != 0 {
		t.Errorf("the next check offered again: %v", got)
	}
	tars = count("tar")
	check(t)
	if count("tar") != tars || count("tree 304") == 0 {
		t.Errorf("asked %v: an unchanged tree is GitHub's 304 and fetches nothing", asked)
	}

	// a library from before uni-switch kept what it offered: every skill
	// beside the installed ones that it hasn't is new
	mu.Lock()
	l, _ = load()
	l.SeenSkills = nil
	l.save()
	mu.Unlock()
	check(t)
	if got := newNames(t); !slices.Equal(got, []string{"dbs-hook", "dbs-xhs-title"}) {
		t.Errorf("with nothing kept: %v", got)
	}

	// none of a repository the library has no skill from any more
	ok(t)(RemoveSkills([]string{"dbs", "dbs-goal", "dbs-save"}))
	if got := newNames(t); len(got) != 0 {
		t.Errorf("after removing the repository's skills: %v", got)
	}
	if err := IgnoreNewSkills([]string{"https://github.com/nobody/x/tree/HEAD/y"}); err == nil {
		t.Error("set aside what no check found, without saying")
	}
}

// Where a check looks: beside the installed skills, in the deepest
// folder holding them all, as the picker would list them.
func TestNewBase(t *testing.T) {
	sk := func(paths ...string) []*Skill {
		var out []*Skill
		for _, p := range paths {
			out = append(out, &Skill{Source: &Source{Kind: "github", Path: p}})
		}
		return out
	}
	for _, c := range []struct {
		paths []string
		want  string
	}{
		{[]string{"skills/engineering/tdd", "skills/engineering/pr"}, "skills/engineering"},
		{[]string{"skills/engineering/tdd", "skills/productivity/teach"}, "skills"},
		{[]string{"skills/a", "other/b"}, ""},
		{[]string{"top"}, ""},
		{[]string{""}, ""},
		{[]string{"a/b/c/d", "a/b/cc/e"}, "a/b"},
	} {
		if got := newBase(sk(c.paths...)); got != c.want {
			t.Errorf("%v: %q, want %q", c.paths, got, c.want)
		}
	}
	tree := &gitTree{}
	for _, p := range []string{"SKILL.md.bak", "skills/a/SKILL.md", "skills/a/b/SKILL.md", "skills/node_modules/x/SKILL.md", "skills/.x/SKILL.md", "skills/c/d/e/f/SKILL.md", "skills/c/d/e/f/g/SKILL.md", "other/z/SKILL.md"} {
		tree.Tree = append(tree.Tree, gitEntry{Path: p, Type: "blob"})
	}
	if got := skillDirsIn(tree, "skills"); !slices.Equal(got, []string{"skills/a", "skills/c/d/e/f"}) {
		t.Errorf("skills in the tree: %v", got)
	}
}

// char1eslu's report (#1217): skills from majiayu000/claude-arsenal and
// two MCP servers, each switched off for every agent, were saved with
// "agents": null; a check then offered the repository's other skills with
// agents null, and the page threw drawing them. Every list of agents is
// [] when it is empty: in library.json, and in what the page is sent.
func TestNoAgentsIsEmptyNotNull(t *testing.T) {
	sandbox(t)
	fakeSkillRepo(t, map[string]string{
		"skills/codex-fluent/SKILL.md":        "---\nname: codex-fluent\ndescription: Fluent\n---\n",
		"skills/codex-retrospective/SKILL.md": "---\nname: codex-retrospective\ndescription: Retro\n---\n",
		"skills/skill-usage-stats/SKILL.md":   "---\nname: skill-usage-stats\ndescription: Stats\n---\n",
	})
	ok(t)(InstallSkills("majiayu000/claude-arsenal", []string{"skills/codex-fluent", "skills/codex-retrospective"}, []string{"claude"}))
	ok(t)(SkillAgents("codex-fluent", []string{}))
	ok(t)(SkillAgents("codex-retrospective", []string{}))
	ok(t)(SaveServer("", Server{Name: "stitch", Transport: "http", URL: "https://stitch.googleapis.com/mcp", Agents: []string{"gemini"}}))
	ok(t)(ServerAgents("stitch", []string{}))
	mu.Lock()
	l, _ := load()
	l.SeenSkills = nil // the third is new to the next check
	l.save()
	mu.Unlock()
	check(t)

	b, err := os.ReadFile(path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"agents": null`) {
		t.Errorf("library.json has agents null:\n%s", b)
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.NewSkills) != 1 || v.NewSkills[0].Name != "skill-usage-stats" {
		t.Fatalf("new skills: %+v", v.NewSkills)
	}
	page, _ := json.Marshal(v)
	if strings.Contains(string(page), `"agents":null`) {
		t.Errorf("the page is sent agents null: %s", page)
	}

	// added, it is on the agents the others are on: none, as []
	ok(t)(AddNewSkills([]string{v.NewSkills[0].ID}))
	b, _ = os.ReadFile(path())
	if !strings.Contains(string(b), `"skill-usage-stats"`) || strings.Contains(string(b), `"agents": null`) {
		t.Errorf("after adding the new one:\n%s", b)
	}
}

// UFO404's report (#1964): the market lists ui-ux-pro-max from
// nextlevelbuilder/ui-ux-pro-max-skill, whose SKILL.md is only in
// .claude/skills; its other skills are both there and in cli/assets/skills.
// The install found no ui-ux-pro-max ("has no skill ui-ux-pro-max any
// more"). The paths are the repository's own.
func TestMarketInstallsASkillKeptInClaudesFolder(t *testing.T) {
	sandbox(t)
	files := map[string]string{
		".claude/skills/ui-ux-pro-max/SKILL.md":          "---\nname: ui-ux-pro-max\ndescription: \"UI/UX design intelligence for web, mobile, and desktop.\"\n---\n\n# UI/UX Pro Max - Design Intelligence\n",
		".claude/skills/ui-ux-pro-max/scripts/search.py": "print('hi')\n",
		".github/workflows/ci.yml":                       "on: push\n",
		"skills/.hidden/SKILL.md":                        "---\nname: hidden\ndescription: not a skill folder\n---\n",
		".claude-plugin/x/SKILL.md":                      "---\nname: plugin-x\ndescription: not in a skills folder\n---\n",
	}
	for _, n := range []string{"banner-design", "brand", "design-system", "design", "slides", "ui-styling"} {
		md := "---\nname: " + n + "\ndescription: Brand voice, visual identity.\n---\n"
		files[".claude/skills/"+n+"/SKILL.md"] = md
		files["cli/assets/skills/"+n+"/SKILL.md"] = md
	}
	fakeSkillRepo(t, files)

	p, err := ProbeSkills("nextlevelbuilder/ui-ux-pro-max-skill")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range p.Candidates {
		paths = append(paths, c.Path)
	}
	want := []string{"cli/assets/skills/banner-design", "cli/assets/skills/brand", "cli/assets/skills/design", "cli/assets/skills/design-system", "cli/assets/skills/slides", "cli/assets/skills/ui-styling", ".claude/skills/ui-ux-pro-max"}
	if !slices.Equal(paths, want) {
		t.Errorf("candidates %v, want %v", paths, want)
	}

	r, err := InstallMarketSkill("nextlevelbuilder/ui-ux-pro-max-skill", "ui-ux-pro-max", []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Installed, []string{"ui-ux-pro-max"}) {
		t.Errorf("installed %v", r.Installed)
	}
	if _, err := os.Stat(skillDir("ui-ux-pro-max") + "/scripts/search.py"); err != nil {
		t.Errorf("the skill's files weren't installed: %v", err)
	}

	// the check for new skills reads the tree the same way
	tree := &gitTree{}
	for p := range files {
		tree.Tree = append(tree.Tree, gitEntry{Path: p, Type: "blob"})
	}
	sort.Slice(tree.Tree, func(i, j int) bool { return tree.Tree[i].Path < tree.Tree[j].Path })
	got := skillDirsIn(tree, "")
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("skills in the tree %v, want %v", got, want)
	}
}

// 碳碳双键's report on Discord: tw93/waza keeps its eight skills in skills/
// and again in plugins/waza/skills/ (some copies behind the top ones), and
// typing "tw93/waza" listed 16 skills, each twice. The paths are the
// repository's own.
func TestRepoWithAPluginCopyListsEachSkillOnce(t *testing.T) {
	sandbox(t)
	names := []string{"check", "health", "hunt", "learn", "read", "think", "ui", "write"}
	files := map[string]string{}
	for _, n := range names {
		files["skills/"+n+"/SKILL.md"] = "---\nname: " + n + "\ndescription: the top one\n---\n"
		files["plugins/waza/skills/"+n+"/SKILL.md"] = "---\nname: " + n + "\ndescription: the plugin's copy\n---\n"
	}
	fakeSkillRepo(t, files)

	p, err := ProbeSkills("tw93/waza")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range p.Candidates {
		paths = append(paths, c.Path)
		if c.Description != "the top one" {
			t.Errorf("%s: %q, want the top one", c.Path, c.Description)
		}
	}
	var want []string
	for _, n := range names {
		want = append(want, "skills/"+n)
	}
	if !slices.Equal(paths, want) {
		t.Errorf("candidates %v, want %v", paths, want)
	}

	tree := &gitTree{}
	for p := range files {
		tree.Tree = append(tree.Tree, gitEntry{Path: p, Type: "blob"})
	}
	sort.Slice(tree.Tree, func(i, j int) bool { return tree.Tree[i].Path < tree.Tree[j].Path })
	if got := skillDirsIn(tree, ""); !slices.Equal(got, want) {
		t.Errorf("skills in the tree %v, want %v", got, want)
	}
}

// stablyai/orca's file list answers 200 with ~10 MB of JSON, over the 8 MB
// apiGet once read; the cut-off body then failed to parse as "unexpected
// end of JSON input". A list that size is read and understood.
func TestRepoTreeBigList(t *testing.T) {
	// ~10 MB of entries, the shape GitHub answers for orca
	tree := &gitTree{}
	for i := 0; i < 60_000; i++ {
		tree.Tree = append(tree.Tree, gitEntry{
			Path: fmt.Sprintf("apps/desktop/src/renderer/components/panels/sidebar/file%05d.ts", i),
			Type: "blob", Mode: "100644", SHA: "e488653ba64c6cfc3551ec46454b965421d50176", Size: 321,
		})
	}
	tree.Tree = append(tree.Tree, gitEntry{Path: "skills/orca/SKILL.md", Type: "blob", Mode: "100644", SHA: "a438021d7b90192e2c0c4802e6cff1b0df8bb203"})
	b, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) <= 8<<20 {
		t.Fatalf("fixture is %d bytes, not over the old cap", len(b))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(b)
	}))
	defer srv.Close()
	oldA := githubAPI
	githubAPI = srv.URL
	defer func() { githubAPI = oldA }()

	got, err := repoTree("stablyai/orca", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tree) != len(tree.Tree) {
		t.Fatalf("read %d of %d entries", len(got.Tree), len(tree.Tree))
	}
	if dirs := skillDirsIn(got, "skills"); !slices.Equal(dirs, []string{"skills/orca"}) {
		t.Errorf("skills in it: %v", dirs)
	}
}

// zhaoxuya520/reverse-skill keeps a routing SKILL.md in its skills folder
// (#4572): reverse-skill-router, an entry that routes to the skills beside
// it. The folder is a skill and a collection at once, and only the router
// was listed: every skill under it was hidden behind it. A folder that
// holds several skills beside its own SKILL.md is both, and offers them
// all; a lone SKILL.md inside another skill (skills/pentest-tools/
// src-hunter here, skills/dbs-save/inner in TestCheckSkillsFindsNewOnes)
// still isn't a skill of its own.
func TestRepoWithARoutingSkillListsItsSkillsToo(t *testing.T) {
	sandbox(t)
	files := map[string]string{
		"skills/SKILL.md":                          "---\nname: reverse-skill-router\ndescription: Routes to the skills beside it\n---\n",
		"skills/apk-reverse/SKILL.md":              "---\nname: apk-reverse\ndescription: APKs\n---\n",
		"skills/ida-reverse/SKILL.md":              "---\nname: ida-reverse\ndescription: Binaries\n---\n",
		"skills/pentest-tools/SKILL.md":            "---\nname: pentest-tools\ndescription: Tools\n---\n",
		"skills/pentest-tools/src-hunter/SKILL.md": "---\nname: src-hunter\ndescription: Sources\n---\n",
	}
	fakeSkillRepo(t, files)

	// the repository's root and its skills folder offer the same: the
	// router and the skills under it
	want := []string{"skills", "skills/apk-reverse", "skills/ida-reverse", "skills/pentest-tools"}
	for _, in := range []string{"zhaoxuya520/reverse-skill", "https://github.com/zhaoxuya520/reverse-skill/tree/HEAD/skills"} {
		p, err := ProbeSkills(in)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, c := range p.Candidates {
			paths = append(paths, c.Path)
		}
		sort.Strings(paths)
		if !slices.Equal(paths, want) {
			t.Errorf("%s: candidates %v, want %v", in, paths, want)
		}
	}

	// the check for new skills reads the tree the same way
	tree := &gitTree{}
	for p := range files {
		tree.Tree = append(tree.Tree, gitEntry{Path: p, Type: "blob"})
	}
	sort.Slice(tree.Tree, func(i, j int) bool { return tree.Tree[i].Path < tree.Tree[j].Path })
	if got := skillDirsIn(tree, ""); !slices.Equal(got, want) {
		t.Errorf("skills in the tree %v, want %v", got, want)
	}
}
