package agent

import (
	"testing"
	"time"
)

// TestRunningListedOnceInAWhile: /api/state asked pgrep again on every
// call; a pattern asked again within runningFor is not listed again, an
// age read later has grown, a restart's own look replaces what was kept,
// and a test's fake is asked each time.
func TestRunningListedOnceInAWhile(t *testing.T) {
	pat := `no-such-process-[0-9]{9}-lately`
	runs := listRuns.Load()
	for range 6 {
		if ps := runningLately(pat); len(ps) != 0 {
			t.Fatalf("found %+v", ps)
		}
	}
	if n := listRuns.Load() - runs; n != 1 {
		t.Fatalf("listed %d times in a row, want 1", n)
	}

	lately.put(pat, time.Now(), []runningProc{{10 * time.Second, "codex app-server"}})
	ps := runningLately(pat)
	if len(ps) != 1 || ps[0].cmd != "codex app-server" || ps[0].up < 10*time.Second || ps[0].up > 12*time.Second {
		t.Fatalf("kept copy read back as %+v", ps)
	}

	// a restart waiting for the app to end looks now, and finds it gone
	runs = listRuns.Load()
	listRunning(pat)
	if ps := runningLately(pat); len(ps) != 0 || listRuns.Load() != runs {
		t.Fatalf("after a fresh look: %+v, %d listings", ps, listRuns.Load()-runs)
	}

	lately.put(pat, time.Now().Add(-runningFor-time.Second), []runningProc{{time.Second, "codex"}})
	if ps := runningLately(pat); len(ps) != 0 || listRuns.Load() != runs+1 {
		t.Fatalf("an old listing was used: %+v, %d listings", ps, listRuns.Load()-runs)
	}

	defer func(r func(string) []runningProc) { running = r }(running)
	asked := 0
	running = func(string) []runningProc { asked++; return nil }
	runningLately(pat)
	runningLately(pat)
	if asked != 2 {
		t.Fatalf("a test's running asked %d times, want 2", asked)
	}
}
