package usage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// advanceHistory writes a Codex call, each with its gateway call, 368,
// 40, 20, 10, 5, 2 and 0 days before Sunday 2026-10-04 noon, and holds
// the clock there. The call two days back is after that day's cut and
// before the next day's; the one 368 days back is in that day's heatmap
// and not in the next day's, which begins a week later.
func advanceHistory(t *testing.T) (yesterday, today time.Time) {
	pageHome(t)
	pairAtOnce(t)
	yesterday = holdClock(t, time.Date(2026, 10, 4, 12, 0, 0, 0, time.Local))
	for i, days := range []int{368, 40, 20, 10, 5, 2, 0} {
		id := string(rune('a' + i))
		at := yesterday.AddDate(0, 0, -days).Add(-time.Hour)
		path := filepath.Join(sessions.CodexDir(), "sessions", at.Format("2006/01/02"), "rollout-"+at.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
		writeCodexCall(t, path, id, "s"+id, `"vscode"`, "resp-"+id, "m", "low", at, 100+i, 10)
		Append(Record{Time: at.Add(time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 100 + i, Output: 10, Status: 200, ResponseID: "resp-" + id, Session: "s" + id})
	}
	return yesterday, yesterday.AddDate(0, 0, 1)
}

// pairedBelow fails unless every pairing since from paired fewer than all.
func pairedBelow(t *testing.T, what string, paired []int, from, all int) {
	t.Helper()
	if len(paired) == from {
		t.Fatalf("%s: nothing paired", what)
	}
	for _, n := range paired[from:] {
		if n >= all {
			t.Fatalf("%s: paired %d of %d chunks, %v", what, n, all, paired[from:])
		}
	}
}

// The first start of a day pairs only the last days' chunks for the
// heatmap: the days kept at the day before's cut stand, and the day
// between the two cuts is added (#2278). The days are a cold pairing's.
func TestHeatmapDaysAdvanceACut(t *testing.T) {
	_, today := advanceHistory(t)
	paired := []int{}
	heatPairing = func(n int) { paired = append(paired, n) }
	t.Cleanup(func() { heatPairing = nil })
	restart := func() {
		requestCache.Lock()
		requestCache.heatParts, requestCache.heatOld, requestCache.heats, requestCache.pairs, requestCache.pages = nil, nil, nil, nil, map[pageKey]RequestPage{}
		requestCache.Unlock()
	}
	restart()
	t.Cleanup(restart)
	HeatmapOf(Filter{})
	all := paired[0]
	holdClock(t, today)
	restart()
	from := len(paired)
	warm := HeatmapOf(Filter{})
	pairedBelow(t, "the next day", paired, from, all)
	if e, _ := heatDiskEntryRead(); !e.Cut.Equal(time.Date(2026, 10, 3, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("the next day's days were written at cut %v", e.Cut)
	}
	restart()
	from = len(paired)
	if again := HeatmapOf(Filter{}); !reflect.DeepEqual(again, warm) {
		t.Fatalf("started again: %+v, was %+v", again, warm)
	}
	pairedBelow(t, "started again", paired, from, all)
	restart()
	os.Remove(heatDiskPath())
	cold := HeatmapOf(Filter{})
	if !reflect.DeepEqual(warm, cold) {
		t.Fatalf("the next day %+v, cold %+v", warm, cold)
	}
	if len(cold.Days) != 6 {
		t.Fatalf("cold days %+v", cold.Days)
	}
}

// The first start of a day pairs only the last days' chunks for a page
// (#2278), and builds the page a pairing of every chunk does.
func TestPageCellsAdvanceACut(t *testing.T) {
	_, today := advanceHistory(t)
	paired := []int{}
	pageSplitting = func(n int) { paired = append(paired, n) }
	t.Cleanup(func() { pageSplitting = nil })
	restart := func() {
		requestCache.Lock()
		requestCache.parts, requestCache.pageOld, requestCache.pairs, requestCache.pages = nil, nil, nil, map[pageKey]RequestPage{}
		requestCache.Unlock()
	}
	restart()
	t.Cleanup(restart)
	QueryPage(All, Filter{}, 0, 1)
	all := paired[0]
	holdClock(t, today)
	restart()
	from := len(paired)
	warm := QueryPage(All, Filter{}, 0, 1)
	pairedBelow(t, "the next day", paired, from, all)
	restart()
	from = len(paired)
	if again := QueryPage(All, Filter{}, 0, 1); !reflect.DeepEqual(again, warm) {
		t.Fatalf("started again: %+v, was %+v", again, warm)
	}
	pairedBelow(t, "started again", paired, from, all)
	restart()
	os.Remove(pageDiskPath())
	cold := QueryPage(All, Filter{}, 0, 1)
	if !reflect.DeepEqual(warm, cold) {
		t.Fatalf("the next day %+v, cold %+v", warm, cold)
	}
	if cold.Total != 7 {
		t.Fatalf("cold page: %d rows", cold.Total)
	}
}

// The first start of a day pairs only the last days' chunks for Codex's
// effort (#2278), and tells the effort a pairing of every chunk does.
func TestCodexEffortAdvancesACut(t *testing.T) {
	_, today := advanceHistory(t)
	paired := []int{}
	effortPairing = func(n int) { paired = append(paired, n) }
	t.Cleanup(func() { effortPairing = nil })
	restart := func() {
		codexEffort.Lock()
		codexEffort.periods = map[Period]*effortEntry{}
		codexEffort.Unlock()
		codexEffortBuilding.Lock()
		effortOlds = map[Period]*effortOld{}
		codexEffortBuilding.Unlock()
		requestCache.Lock()
		requestCache.pairs = nil
		requestCache.Unlock()
	}
	restart()
	t.Cleanup(restart)
	CodexModelEffort(All)
	all := paired[0]
	holdClock(t, today)
	restart()
	from := len(paired)
	warm := CodexModelEffort(All)
	pairedBelow(t, "the next day", paired, from, all)
	restart()
	os.Remove(effortDiskPath())
	cold := CodexModelEffort(All)
	if len(cold) != 7 || len(warm) != len(cold) {
		t.Fatalf("the next day %d threads, cold %d", len(warm), len(cold))
	}
	for id, c := range cold {
		w := warm[id]
		if len(w) != len(c) {
			t.Fatalf("thread %s: the next day %+v, cold %+v", id, w, c)
		}
		for i := range c {
			if w[i].Tokens != c[i].Tokens || w[i].Priced != c[i].Priced {
				t.Fatalf("thread %s: the next day %+v, cold %+v", id, w, c)
			}
		}
	}
}
