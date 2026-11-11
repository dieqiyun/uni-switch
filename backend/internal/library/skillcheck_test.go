package library

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake GitHub: codeload's tarballs and the API's last commit to a
// folder, which has lost y, and says the API's rate limit is spent once
// limited is set.
func TestCheckSkills(t *testing.T) {
	sandbox(t)
	var amu sync.Mutex
	asked := map[string]int{}
	files := map[string]string{
		"skills/pdf/SKILL.md":  "---\nname: pdf\ndescription: PDFs\n---\n",
		"skills/docx/SKILL.md": "---\nname: docx\ndescription: Word\n---\n",
		"x/SKILL.md":           "---\nname: x\n---\n",
		"y/SKILL.md":           "---\nname: y\n---\n",
	}
	shas := map[string]string{"skills/pdf": "p1", "skills/docx": "d1", "x": "x1"}
	limited := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amu.Lock()
		defer amu.Unlock()
		if repo, ok := strings.CutPrefix(r.URL.Path, "/tar/"); ok {
			asked["tar "+repo]++
			w.Write(tarball(t, files))
			return
		}
		if strings.Contains(r.URL.Path, "/git/trees/") {
			w.WriteHeader(404) // what the repositories added since is TestCheckSkillsFindsNewOnes's
			return
		}
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/commits")
		asked["api "+repo]++
		if r.URL.Query().Get("per_page") != "1" {
			t.Errorf("asked for %s", r.URL.RawQuery)
		}
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "2000000000")
			w.WriteHeader(403)
			w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		sha := shas[r.URL.Query().Get("path")]
		if sha == "" {
			w.Write([]byte("[]"))
			return
		}
		fmt.Fprintf(w, `[{"sha":%q,"html_url":"https://github.com/%s/commit/%s","commit":{"message":"Change %s\n\nat length","committer":{"date":"2026-09-01T10:00:00Z"}}}]`, sha, repo, sha, sha)
	}))
	defer srv.Close()
	oldT, oldA := tarballURL, githubAPI
	tarballURL = func(repo, ref string) string { return srv.URL + "/tar/" + repo }
	githubAPI = srv.URL
	defer func() { tarballURL, githubAPI = oldT, oldA }()

	ok(t)(InstallSkills("owner/checked", []string{"skills/pdf", "skills/docx"}, []string{"claude"}))
	ok(t)(InstallSkills("owner/more", []string{"x", "y"}, []string{"claude"}))
	l, _ := load()
	if l.skill("pdf").Hash == "" || l.skill("pdf").Commit != "" {
		t.Errorf("installed: %+v", l.skill("pdf"))
	}

	// docx changed on GitHub since; pdf didn't, which isn't known by its
	// commit yet, so the repository is fetched to compare its files
	files["skills/docx/SKILL.md"] = "---\nname: docx\ndescription: Word, better\n---\n"
	clear(asked)
	got := check(t)
	if c := got["pdf"]; c.Status != "current" || c.Commit != "p1" || c.Message != "Change p1" || c.Date != "2026-09-01T10:00:00Z" {
		t.Errorf("pdf: %+v", c)
	}
	if c := got["docx"]; c.Status != "update" || c.URL != "https://github.com/owner/checked/commits/HEAD/skills/docx" {
		t.Errorf("docx: %+v", c)
	}
	if c := got["x"]; c.Status != "current" {
		t.Errorf("x: %+v", c)
	}
	if c := got["y"]; c.Status != "unknown" || c.Error != "owner/more has nothing at y any more" {
		t.Errorf("y: %+v", c)
	}
	if asked["api owner/checked"] != 2 || asked["tar owner/checked"] != 1 {
		t.Errorf("asked %v", asked)
	}
	l, _ = load()
	if l.skill("pdf").Commit != "p1" || l.skill("docx").Commit != "" {
		t.Errorf("kept: pdf %q docx %q", l.skill("pdf").Commit, l.skill("docx").Commit)
	}
	v, _ := Read(nil)
	if i := slices.IndexFunc(v.Skills, func(s SkillView) bool { return s.Name == "docx" }); v.Skills[i].Check == nil || v.Skills[i].Check.Status != "update" {
		t.Errorf("the page wasn't told: %+v", v.Skills[i])
	}

	// pdf is known by its commit now: a new one is an update, told
	// without fetching the repository again
	shas["skills/pdf"] = "p2"
	files["skills/docx/SKILL.md"] = "---\nname: docx\ndescription: Word\n---\n"
	clear(asked)
	got = check(t)
	if c := got["pdf"]; c.Status != "update" || c.URL != "https://github.com/owner/checked/compare/p1...p2" {
		t.Errorf("pdf: %+v", c)
	}
	if c := got["docx"]; c.Status != "current" {
		t.Errorf("docx: %+v", c)
	}
	files["skills/pdf/SKILL.md"] = "---\nname: pdf\ndescription: PDFs, better\n---\n"

	// only the skills named are updated, and they're checked afresh after
	res, err := UpdateSomeSkills([]string{"pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Updated, []string{"pdf"}) {
		t.Errorf("updated %v", res.Updated)
	}
	v, _ = Read(nil)
	for _, s := range v.Skills {
		if s.Name == "pdf" && (s.Description != "PDFs, better" || s.Check != nil) {
			t.Errorf("pdf after update: %+v", s)
		}
		if s.Name == "docx" && s.Check == nil {
			t.Error("docx's check went with pdf's update")
		}
	}
	l, _ = load()
	if s := l.skill("pdf"); s.Commit != "" || s.Hash != hashDir(skillDir("pdf")) {
		t.Errorf("pdf kept %+v", s)
	}
	if got = check(t); got["pdf"].Status != "current" {
		t.Errorf("pdf after update: %+v", got["pdf"])
	}

	// one installed before uni-switch kept a hash is compared by its files
	mu.Lock()
	l, _ = load()
	l.skill("docx").Hash, l.skill("docx").Commit = "", ""
	l.save()
	mu.Unlock()
	if got = check(t); got["docx"].Status != "current" {
		t.Errorf("old docx: %+v", got["docx"])
	}
	if l, _ = load(); l.skill("docx").Commit != "d1" || l.skill("docx").Hash == "" {
		t.Errorf("old docx kept %+v", l.skill("docx"))
	}

	// once GitHub limits requests, every skill is unknown, and each
	// repository asked about once at most
	limited = true
	clear(asked)
	got = check(t)
	for _, n := range []string{"pdf", "docx", "x", "y"} {
		if c := got[n]; c.Status != "unknown" || c.Error != (errLimited{Until: time.Unix(2000000000, 0)}).Error() {
			t.Errorf("%s: %+v", n, c)
		}
	}
	if n := asked["api owner/checked"] + asked["api owner/more"]; n > 2 {
		t.Errorf("asked GitHub %d times once limited", n)
	}

	if _, err := UpdateSomeSkills(nil); err == nil {
		t.Error("updated nothing without saying")
	}
}

func check(t *testing.T) map[string]SkillCheck {
	t.Helper()
	list, err := CheckSkills()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]SkillCheck{}
	for _, c := range list {
		m[c.Name] = c
	}
	return m
}

// An answer over the cap is an error that says so, never a cut body for a
// JSON parse to trip on; a cut-off answer is a read error; a rate limit is
// told from the status and headers even then; and an answer the cap lets
// through but cacheBody doesn't isn't kept for a 304 to answer with.
func TestApiGetWholeAnswer(t *testing.T) {
	var mu sync.Mutex
	var ifNone []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ifNone = append(ifNone, r.Header.Get("If-None-Match"))
		mu.Unlock()
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, strings.Repeat(`{"path":"a","type":"blob"},`, 1<<20)) // ~27 MB
	}))
	defer srv.Close()

	if _, _, _, err := apiGet(srv.URL, 8<<20); err == nil || !strings.Contains(err.Error(), "over 8 MB") {
		t.Fatalf("over the cap: %v", err)
	}
	for range 2 {
		if _, _, _, err := apiGet(srv.URL, 64<<20); err != nil {
			t.Fatalf("whole answer under the cap: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ifNone) != 3 || slices.Contains(ifNone, `"v1"`) {
		t.Errorf("an over-cacheBody answer was kept for a 304: %q", ifNone)
	}

	// a connection that drops mid-body is a read error, not a part body
	drop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, `{"sha":`)
	}))
	defer drop.Close()
	if _, _, _, err := apiGet(drop.URL, 8<<20); err == nil || !strings.Contains(err.Error(), "couldn't read GitHub's answer") {
		t.Fatalf("dropped connection: %v", err)
	}

	// so is a cut-off rate limit, but it's told from the status and
	// headers: CheckSkills stops asking once GitHub limits it
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "2000000000")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":`)
	}))
	defer limited.Close()
	_, _, _, err := apiGet(limited.URL, 8<<20)
	var el errLimited
	if !errors.As(err, &el) {
		t.Fatalf("cut-off rate limit: %v", err)
	}

	// a non-200's size never masks the status the caller reports
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		fmt.Fprint(w, strings.Repeat("bad gateway ", 1<<20)) // ~12 MB
	}))
	defer bad.Close()
	if _, code, _, err := apiGet(bad.URL, 8<<20); err != nil || code != 502 {
		t.Fatalf("502 over the cap: code %d, err %v", code, err)
	}
}

// An empty 200 is said so, not kept for a 304 to answer nothing with
// again (#3045).
func TestAPIGetEmptyAnswer(t *testing.T) {
	sandbox(t)
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer empty.Close()
	if _, _, _, err := apiGet(empty.URL, 8<<20); err == nil {
		t.Error("an empty answer wasn't noticed")
	} else if !strings.Contains(err.Error(), "GitHub answered nothing") {
		t.Errorf("said: %v", err)
	}
}
