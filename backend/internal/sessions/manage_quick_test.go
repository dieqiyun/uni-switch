package sessions

import (
	"sync/atomic"
	"testing"
	"time"
)

// The Sessions page answers before a first read of its files (hey_boyce on
// X: its placeholder rows stood for a long time on a first open). Its rows
// come from a glance at each file's first lines, say they are pending, and
// the whole read runs behind the answer; the next ask has their counts.
func TestManageAnswersBeforeFirstReads(t *testing.T) {
	setup(t)
	old := quickBytes
	quickBytes = 0 // as a long history: every file over the bar
	release := make(chan struct{})
	var parses atomic.Int64
	parseHook = func() { parses.Add(1); <-release }
	opened := false
	open := func() {
		if !opened {
			opened = true
			close(release)
		}
	}
	t.Cleanup(func() { open(); behindRuns.Wait(); parseHook, quickBytes = nil, old })

	claude := func([]AgentCount, map[string]map[string]bool) string { return "claude" }
	got := make(chan ManageView, 1)
	go func() { got <- Manage(map[string]bool{"codex": true, "claude": true}, claude) }()
	var v ManageView
	select {
	case v = <-got:
	case <-time.After(30 * time.Second): // a fail-safe: the old code never answers
		n := parses.Load()
		open()
		<-got // ended before the test's folder goes
		t.Fatalf("Manage waited for the whole read (%d files being read)", n)
	}
	if len(v.Sessions) < 2 {
		t.Fatalf("listed %d sessions before the read", len(v.Sessions))
	}
	for _, m := range v.Sessions {
		if !m.Pending {
			t.Fatalf("%s not pending, read whole before the answer", m.ID)
		}
		if m.Last.IsZero() {
			t.Fatalf("glanced row %+v has no time", m.Session)
		}
		if m.Input+m.Output != 0 {
			t.Fatalf("a glance counted tokens: %+v", m.Tokens)
		}
	}
	glanced := map[string]Managed{}
	for _, m := range v.Sessions {
		glanced[m.ID] = m
	}
	named := 0
	for _, m := range glanced {
		if m.Cwd != "" && m.Title != "" {
			named++
		}
	}
	if named == 0 {
		t.Fatalf("no glanced row has a folder and title: %+v", v.Sessions)
	}
	if !v.Native["codex"]["01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd"] {
		t.Fatalf("codex ids from the glance: %v", v.Native["codex"])
	}
	// no glance stays in the cache, nor goes into a save
	mu.Lock()
	for _, m := range v.Sessions {
		if s := cache[m.Path]; s != nil {
			mu.Unlock()
			t.Fatalf("%s: a glance left in the cache", m.Path)
		}
	}
	if len(savable()) != 0 {
		mu.Unlock()
		t.Fatal("a glance would be saved")
	}
	mu.Unlock()

	// asked again while the read is under way: still answered, still pending
	go func() { got <- Manage(map[string]bool{"codex": true}, claude) }()
	select {
	case v = <-got:
	case <-time.After(30 * time.Second):
		open()
		<-got
		t.Fatal("a second ask waited for the read under way")
	}
	if len(v.Sessions) == 0 || !v.Sessions[0].Pending {
		t.Fatalf("second ask: %+v", v.Sessions)
	}

	open()
	behindRuns.Wait()
	v = Manage(map[string]bool{"codex": true}, claude)
	spent := false
	for _, m := range v.Sessions {
		if m.Pending {
			t.Fatalf("%s still pending after the read", m.ID)
		}
		spent = spent || m.Input+m.Output > 0 && m.Messages > 0
		// the glance showed what the whole read shows
		g, ok := glanced[m.ID]
		if !ok || g.Cwd != m.Cwd || g.Title != m.Title || g.Path != m.Path {
			t.Fatalf("glanced %q %q %q, read %q %q %q", g.ID, g.Cwd, g.Title, m.ID, m.Cwd, m.Title)
		}
	}
	if len(v.Sessions) != len(glanced) {
		t.Fatalf("%d glanced, %d read", len(glanced), len(v.Sessions))
	}
	if !spent {
		t.Fatalf("read whole and nothing counted: %+v", v.Sessions)
	}
	if !v.Native["codex"]["01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd"] {
		t.Fatalf("codex ids after the read: %v", v.Native["codex"])
	}
}
