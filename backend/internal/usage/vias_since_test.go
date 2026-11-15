package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The Sessions list, each agent's manage page and a session's detail each
// ask Vias from a since of their own. One kept since was built again from
// the whole log at almost every ask, ~280 MB a time over ~720k calls, on
// every poll of an open page (#2776). One index now answers every since a
// session's calls all came after, and a session with calls before its
// since still counts only those after it.
func TestViasAnswerEverySinceFromOneIndex(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	const sessionCount = 20000
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(Path())
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	now := Clock()
	write := func(r Record) {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	for i := range sessionCount {
		write(Record{Time: now.Add(-time.Duration(sessionCount-i) * time.Second), Agent: "pi", Session: fmt.Sprintf("s%d", i), Provider: "p", Model: "m", Input: 10, Status: 200})
	}
	// one session with a call on each side of a since
	write(Record{Time: now.Add(-time.Hour), Agent: "pi", Session: "both", Provider: "p", Model: "m", Input: 10, Status: 200})
	write(Record{Time: now, Agent: "pi", Session: "both", Provider: "p", Model: "m", Input: 10, Status: 200})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := Vias(now.Add(-time.Minute)).Of("pi|both"); len(got) != 1 || got[0].Calls != 1 {
		t.Fatalf("since a minute ago: %+v, want the one call after it", got)
	}
	if got := Vias(time.Time{}).Of("pi|both"); len(got) != 1 || got[0].Calls != 2 {
		t.Fatalf("since ever: %+v, want both calls", got)
	}
	// the last 60 sessions, a second apart up to a second ago, and "both"
	if n := Vias(now.Add(-time.Minute)).Len(); n != 61 {
		t.Fatalf("%d sessions with calls since a minute ago, want 61", n)
	}
	if n := Vias(time.Time{}).Len(); n != sessionCount+1 {
		t.Fatalf("%d sessions, want %d", n, sessionCount+1)
	}
	// the list, a manage page and a detail, asking in turn
	sinces := []time.Time{
		now.Add(-(sessionCount + 60) * time.Second),
		now.Add(-(sessionCount/2 + 60) * time.Second),
		now.Add(-(sessionCount - 10 + 60) * time.Second),
	}
	key := fmt.Sprintf("pi|s%d", sessionCount-10)
	for _, since := range sinces {
		if got := Vias(since).Of(key); len(got) != 1 || got[0].Calls != 1 {
			t.Fatalf("since %v: %+v", since, got)
		}
	}
	const asks = 30
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range asks {
		Vias(sinces[i%len(sinces)]).Of(key)
	}
	runtime.ReadMemStats(&after)
	// building the lists of 20000 sessions is several MB; the log's check
	// and one list are tens of KB
	per := (after.TotalAlloc - before.TotalAlloc) / asks
	t.Logf("an ask allocated %d bytes", per)
	if per > 256<<10 {
		t.Fatalf("an ask allocated %d bytes: the lists were built again", per)
	}
}
