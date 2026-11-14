package usage

import (
	"testing"
	"time"
)

// What uni-switch reads ahead as it starts leaves the tray panel's first ask,
// today's summary in this computer's zone as /api/usage?period=today asks
// for it, nothing to parse or work out: the cold parse of the whole log
// was what its numbers waited for on the first open (CyberCaius).
func TestReadAheadLeavesTodayNothingToRead(t *testing.T) {
	pageHome(t)
	holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	settleLogClock(t)
	historyLog(t, 2000)
	counts := func() (int, int) {
		logIndex.Lock()
		reads := logIndex.reads
		logIndex.Unlock()
		summaries.Lock()
		defer summaries.Unlock()
		return reads, summaries.worked
	}
	ReadAhead()
	reads, worked := counts()
	got := SummarizeTopIn(Today, 100, nil)
	if r, w := counts(); r != reads || w != worked {
		t.Fatalf("today's summary after the read ahead parsed the log %d more times and was worked out %d more", r-reads, w-worked)
	}
	if got.Totals.Calls != 1 {
		t.Fatalf("today's calls: %+v", got.Totals)
	}
}
