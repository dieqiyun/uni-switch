package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// GatewaySessions hands every caller the kept projection's sessions, not a
// clone of each one's models and days: the start's first asks (the
// Sessions list, Manage for each agent, Stats) each cloned all of them,
// ~450 MB of copies at once over ~720k calls (#2776).
func TestGatewaySessionsHandOutTheKeptProjection(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	const sessionCount, days = 2000, 10
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(Path())
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	now := Clock()
	for d := range days {
		for i := range sessionCount {
			at := now.AddDate(0, 0, -d).Add(-time.Duration(i) * time.Second)
			if err := enc.Encode(Record{Time: at, Agent: "pi", Session: fmt.Sprintf("s%d", i), Provider: "p", Model: "m", Input: 10, Status: 200}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := GatewaySessions(time.Time{}, nil); len(got) != sessionCount || len(got[0].Daily) < days {
		t.Fatalf("%d sessions, the first with %d days", len(got), len(got[0].Daily))
	}
	const asks = 10
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range asks {
		GatewaySessions(time.Time{}, nil)
	}
	runtime.ReadMemStats(&after)
	per := (after.TotalAlloc - before.TotalAlloc) / asks
	list := uint64(sessionCount) * uint64(unsafe.Sizeof(GatewaySession{}))
	t.Logf("an ask allocated %d bytes; the list it answers is %d", per, list)
	// the list itself, and the log's check
	if per > list+list/2 {
		t.Fatalf("an ask allocated %d bytes, its list is %d", per, list)
	}
}
