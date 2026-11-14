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

// Vias hands every caller the one copy it keeps, not a clone of every
// session's list: a Sessions page reads the few sessions it lists, and
// each ask cloned all of them, ~66 MB a time over ~720k calls (#2776).
// A list read through Of is still the caller's own.
func TestViasHandOutTheKeptCopy(t *testing.T) {
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
	for i := range sessionCount {
		if err := enc.Encode(Record{Time: now.Add(-time.Duration(i) * time.Second), Agent: "pi", Session: fmt.Sprintf("s%d", i), Provider: "p", Model: "m", Input: 10, Status: 200}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	vias := Vias(time.Time{})
	if vias.Len() != sessionCount {
		t.Fatalf("%d sessions, want %d", vias.Len(), sessionCount)
	}
	vias.Of("pi|s1")[0].Calls = 999
	if got := Vias(time.Time{}).Of("pi|s1")[0].Calls; got != 1 {
		t.Fatalf("a caller changed the kept copy: %d calls", got)
	}
	const asks = 20
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range asks {
		Vias(time.Time{}).Of("pi|s7")
	}
	runtime.ReadMemStats(&after)
	// a clone of 20000 lists is a few MB; the log's check and one list
	// are tens of KB
	per := (after.TotalAlloc - before.TotalAlloc) / asks
	t.Logf("an ask allocated %d bytes", per)
	if per > 256<<10 {
		t.Fatalf("an ask allocated %d bytes", per)
	}
}
