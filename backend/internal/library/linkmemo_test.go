package library

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
)

// skillsGiven puts n skills from a folder in the library and gives them to
// every agent found, the shape of a Library page that resolves the same
// folders over and over (#1771).
func skillsGiven(t *testing.T, h string, names ...string) string {
	t.Helper()
	src := filepath.Join(h, "src/skills")
	for _, n := range names {
		skill(t, filepath.Join(src, n), n, "d")
	}
	var agents []string
	for _, tg := range Targets() {
		agents = append(agents, tg.Agent.ID)
	}
	ok(t)(InstallSkills(src, names, agents))
	return src
}

// countLooks counts each look at an entry on the way to a path.
func countLooks(t *testing.T) (looks func() map[string]int) {
	var mu sync.Mutex
	n := map[string]int{}
	t.Cleanup(func(was func(string) (part, bool)) func() { return func() { lookAt = was } }(lookAt))
	lookAt = func(p string) (part, bool) {
		mu.Lock()
		n[p]++
		mu.Unlock()
		return statPart(p)
	}
	return func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		return maps.Clone(n)
	}
}

// oxyuan's Library page took 1.3s a visit on Windows, 68% of it following
// links: one read looked at the same few hundred folders over ten thousand
// times. A read now looks at each one once.
func TestReadLooksAtEachFolderOnce(t *testing.T) {
	h := sandbox(t)
	skillsGiven(t, h, "a1", "a2", "a3", "a4", "a5", "a6")
	looks := countLooks(t)
	if _, err := Glance(nil); err != nil {
		t.Fatal(err)
	}
	n := looks()
	if len(n) == 0 {
		t.Fatal("a glance looked at no folder")
	}
	for p, c := range n {
		if c > 1 {
			t.Errorf("%s looked at %d times in one read", p, c)
		}
	}
	// and nothing is kept past the read: the next one looks again
	if reading.Load() != nil {
		t.Fatal("the read's memo outlived it")
	}
	looks = countLooks(t)
	if _, err := Glance(nil); err != nil {
		t.Fatal(err)
	}
	if len(looks()) == 0 {
		t.Fatal("a second visit took the first one's answers")
	}
}

// A link changed on disk between two visits shows on the second.
func TestLinkChangedBetweenVisitsShows(t *testing.T) {
	h := sandbox(t)
	skillsGiven(t, h, "pdf")
	lib := skillDir("pdf")
	if !linked(lib) {
		t.Skip("a skill from a folder isn't linked here")
	}
	sourceOf := func() string {
		t.Helper()
		v, err := Glance(nil)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(v.Skills, func(s SkillView) bool { return s.Name == "pdf" })
		if i < 0 {
			t.Fatalf("no pdf in %+v", v.Skills)
		}
		return v.Skills[i].Source
	}
	if got, want := sourceOf(), realDir(filepath.Join(h, "src/skills/pdf")); realDir(got) != want {
		t.Fatalf("source %q, want %q", got, want)
	}
	other := filepath.Join(h, "elsewhere/pdf")
	skill(t, other, "pdf", "moved")
	if err := os.Remove(lib); err != nil {
		t.Fatal(err)
	}
	if err := dirLink(other, lib); err != nil {
		t.Fatal(err)
	}
	if got, want := sourceOf(), realDir(other); realDir(got) != want {
		t.Fatalf("after the link moved: source %q, want %q", got, want)
	}
}

// An entry that couldn't be looked at is unknown, not "not there": it is
// looked at again within the same read.
func TestUnreadablePartIsLookedAtAgain(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder this user can't list")
	}
	d := t.TempDir()
	locked := filepath.Join(d, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(locked, "x")
	write(t, p, "")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	forget := readParts()
	defer forget()
	if e := lookPart(p); e.there {
		t.Skip("the locked folder could still be looked in")
	}
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if e := lookPart(p); !e.there {
		t.Fatal("an entry that couldn't be looked at was kept as not there")
	}
	// while one that isn't there is kept for the read
	gone := filepath.Join(d, "gone")
	if lookPart(gone).there {
		t.Fatal("gone is there")
	}
	write(t, gone, "")
	if lookPart(gone).there {
		t.Fatal("the read looked at gone twice")
	}
}
