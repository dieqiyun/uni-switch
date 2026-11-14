package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCodexSessionReadOnAfterAPause: a Codex session its user came back to
// after ten minutes (a long think, a meeting) is read on from where the
// last read ended. Its Codex window was let go after five idle minutes, so
// the next turn read the whole rollout again from its first line, a 21 MB
// one in #4369's run on real rollouts.
func TestCodexSessionReadOnAfterAPause(t *testing.T) {
	d := setupCalls(t)
	path := filepath.Join(d.codex, "sessions", "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl")
	writeLines(t, path, longRollout(0, 40)...)
	paused := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(path, paused, paused); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var starts []int64
	scanHook = func(p string, off int64) {
		if p == path {
			mu.Lock()
			starts = append(starts, off)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { scanHook = nil })
	read := func() []Call {
		t.Helper()
		var calls []Call
		for _, s := range CallSources() {
			if s.Path == path {
				calls = ReadCallSource(s)
			}
		}
		if ss := List(0); len(ss) != 1 {
			t.Fatalf("%d sessions", len(ss))
		}
		return calls
	}
	if cs := read(); len(cs) != 40 {
		t.Fatalf("%d calls", len(cs))
	}
	appendText(t, path, strings.Join(longRollout(40, 41), "\n")+"\n")
	mu.Lock()
	starts = nil
	mu.Unlock()
	if cs := read(); len(cs) != 41 {
		t.Fatalf("%d calls after the turn", len(cs))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) == 0 {
		t.Fatal("the turn wasn't read")
	}
	for _, off := range starts {
		if off == 0 {
			t.Fatalf("the turn after a pause read the rollout from its first line: %v", starts)
		}
	}
}
