package sessions

import (
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// The management page of one agent reads that agent's changed files, and
// those of the agents whose gateway sessions it tells apart, not every
// agent's (#2776): it read them all to build that set, through List(All).
func TestManageReadsOnlyTheAgentsItShows(t *testing.T) {
	_, codex := setup(t)
	claude := func([]AgentCount, map[string]map[string]bool) string { return "claude" }
	Manage(map[string]bool{}, claude)
	now := time.Now()
	err := filepath.WalkDir(filepath.Join(codex, "sessions"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			now = now.Add(time.Second)
			return os.Chtimes(p, now, now)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// refresh's readers each call it, at once
	var parsed atomic.Int64
	parseHook = func() { parsed.Add(1) }
	t.Cleanup(func() { parseHook = nil })

	v := Manage(map[string]bool{}, claude)
	if n := parsed.Load(); n != 0 {
		t.Fatalf("Codex's files changed and Claude Code's page read %d of them", n)
	}
	if v.Agent != "claude" || len(v.Sessions) == 0 {
		t.Fatalf("listed %q with %d sessions", v.Agent, len(v.Sessions))
	}
	counted := false
	for _, a := range v.Agents {
		counted = counted || a.Agent == "codex" && a.Count == 1
	}
	if !counted {
		t.Fatalf("codex not counted: %+v", v.Agents)
	}

	// asked for Codex's ids: its changed files are read, as the id is in them
	v = Manage(map[string]bool{"codex": true, "claude": true}, claude)
	if parsed.Load() == 0 {
		t.Fatal("Codex's ids were asked for and its changed files not read")
	}
	if !v.Native["codex"]["01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd"] {
		t.Fatalf("codex ids %v", v.Native["codex"])
	}
	// the same ids as the agent's list: one nothing was said in too
	var listed, native []string
	for _, m := range v.Sessions {
		listed = append(listed, m.ID)
	}
	for id := range v.Native["claude"] {
		native = append(native, id)
	}
	sort.Strings(listed)
	sort.Strings(native)
	if len(listed) < 2 || len(listed) != len(native) {
		t.Fatalf("claude list %v, native ids %v", listed, native)
	}
	for i := range listed {
		if listed[i] != native[i] {
			t.Fatalf("claude list %v, native ids %v", listed, native)
		}
	}
	if got := NativeIDs(map[string]bool{"codex": true}); len(got) != 1 || len(got["codex"]) != 1 {
		t.Fatalf("NativeIDs: %v", got)
	}
}
