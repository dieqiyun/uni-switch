package usage

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The heatmap's year and the whole history's page pair the same chunks
// once between them: on a cold start each paired every row of them, the
// heatmap's the third pairing of all of them and the last of the
// footprint's peaks (#2278). Only the heatmap's recent days are paired
// again for it.
func TestHeatmapPairsAsAllDoes(t *testing.T) {
	pairAtOnce(t)
	for _, heatFirst := range []bool{false, true} {
		name := "page first"
		if heatFirst {
			name = "heatmap first"
		}
		t.Run(name, func(t *testing.T) {
			pageHome(t)
			settleLogClock(t)
			holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
			historyLog(t, 400)
			n := 0
			pairing = func() { n++ }
			t.Cleanup(func() { pairing = nil })
			var h Heatmap
			if heatFirst {
				h = HeatmapOf(Filter{})
				if n != 2 {
					t.Fatalf("the heatmap paired %d times, want 2: every row and its recent days", n)
				}
				QueryPage(All, Filter{}, 0, 1)
				if n != 2 {
					t.Fatalf("the page of All paired %d times after the heatmap, want 0", n-2)
				}
			} else {
				QueryPage(All, Filter{}, 0, 1)
				if n != 1 {
					t.Fatalf("the page of All paired %d times, want 1", n)
				}
				h = HeatmapOf(Filter{})
				if n != 2 {
					t.Fatalf("the heatmap paired %d times after the page of All, want 1: its recent days", n-1)
				}
			}
			calls := 0
			for _, d := range h.Days {
				calls += d.Calls
			}
			if calls != 400 {
				t.Fatalf("the heatmap counted %d calls, want 400", calls)
			}
		})
	}
}

// Big builds that began together take the pairing the one before them
// made while they waited their turn (holdBigBuild): each took the memo
// when it began, found none, and a cold start paired the whole history
// once for each of them (#2278). Each also read the session files with
// chunks of its own serials, so none was the pairing's the one before
// had paired.
func TestBigBuildsBegunTogetherPairOnce(t *testing.T) {
	pairAtOnce(t)
	pageHome(t)
	settleLogClock(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	historyLog(t, 400)
	for i := range 8 {
		at := now.AddDate(0, 0, -20*i).Add(-time.Hour)
		id := string(rune('a' + i))
		path := filepath.Join(sessions.CodexDir(), "sessions", at.Format("2006/01/02"), "rollout-"+at.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
		writeCodexCall(t, path, id, "s"+id, `"vscode"`, "resp-"+id, "m", "low", at, 100+i, 10)
	}
	rows := bigBuildRows
	bigBuildRows = 1
	t.Cleanup(func() { bigBuildRows = rows })
	var mu sync.Mutex
	n := 0
	pairing = func() {
		mu.Lock()
		n++
		mu.Unlock()
	}
	t.Cleanup(func() { pairing = nil })
	// both have read their rows before either pairs them
	var began sync.WaitGroup
	began.Add(2)
	building = func() {
		began.Done()
		began.Wait()
	}
	t.Cleanup(func() { building = nil })
	var wg sync.WaitGroup
	wg.Go(func() { QueryPage(All, Filter{}, 0, 50) })
	wg.Go(func() { HeatmapOf(Filter{}) })
	wg.Wait()
	if n != 2 {
		t.Fatalf("paired %d times, want 2: every row once, and the heatmap's recent days", n)
	}
}
