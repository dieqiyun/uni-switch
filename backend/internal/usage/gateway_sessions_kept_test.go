package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"
)

// The kept projection holds each session's days and models in lists of
// their own size: grown an item at a time they kept the room append had
// left them (five days took eight), which is most of what a start keeps
// for the Sessions page over ~720k calls (#2776).
func TestGatewayProjectionKeepsNoSpareRoom(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	const sessionCount, days = 3000, 5
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
			at := now.AddDate(0, 0, -d)
			for _, model := range []string{"m1", "m2", "m3", "m4", "m5"}[:1+i%5] {
				if err := enc.Encode(Record{Time: at, Agent: "pi", Session: fmt.Sprintf("s%d", i), Provider: "p", Model: model, Input: 10, Status: 200}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	readLogSnapshot() // kept whether or not a projection is
	gatewayCache.Lock()
	gatewayCache.entry = nil
	gatewayCache.Unlock()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	e := gatewayProjection()
	runtime.GC()
	runtime.ReadMemStats(&after)
	kept := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	items, models := 0, 0
	for _, s := range e.sessions {
		items += len(s.Daily)
		models += len(s.Models)
	}
	if len(e.sessions) != sessionCount || items != sessionCount*days*3 {
		t.Fatalf("%d sessions, %d days", len(e.sessions), items)
	}
	// the lists themselves, and the sessions' list and index beside them
	lists := int64(items)*int64(unsafe.Sizeof(GatewayDayUsage{})) + int64(models)*int64(unsafe.Sizeof(GatewayModel{}))
	beside := int64(sessionCount) * (int64(unsafe.Sizeof(GatewaySession{})) + 64)
	t.Logf("the projection keeps %d bytes; its days and models are %d, its list and index ~%d", kept, lists, beside)
	if kept > lists*13/10+beside {
		t.Fatalf("the projection keeps %d bytes for %d bytes of days and models", kept, lists)
	}
	runtime.KeepAlive(e)
}
