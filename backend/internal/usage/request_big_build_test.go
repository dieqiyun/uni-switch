package usage

import (
	"sync"
	"testing"
	"time"
)

// Two big pages pair their rows one after another, not side by side: on a
// cold start the heatmap's year and the requests' All each held every
// call paired at once, most of the footprint's peak (#2278).
func TestBigPagesPairInTurn(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	historyLog(t, 1034)
	rows := bigBuildRows
	bigBuildRows = 1
	t.Cleanup(func() { bigBuildRows = rows })
	var mu sync.Mutex
	in, most := 0, 0
	pairing = func() {
		mu.Lock()
		in++
		most = max(most, in)
		mu.Unlock()
		time.Sleep(200 * time.Millisecond) // the other page pairs now, if it can
		mu.Lock()
		in--
		mu.Unlock()
	}
	t.Cleanup(func() { pairing = nil })
	var wg sync.WaitGroup
	wg.Go(func() { QueryPage(All, Filter{}, 0, 100) })
	wg.Go(func() { HeatmapOf(Filter{}) })
	wg.Wait()
	if most != 1 {
		t.Fatalf("%d big pages paired at once, want 1", most)
	}
}
