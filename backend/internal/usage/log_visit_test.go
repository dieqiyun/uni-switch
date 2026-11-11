package usage

import (
	"reflect"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// A read of the log decodes its blocks into one buffer, not each into rows
// of its own: the pages' reads allocated ~800 MB of rows a minute while
// calls came in (#2278). It reads every record as decoding each block did.
func TestLogVisitReusesItsRows(t *testing.T) {
	pageHome(t)
	holdClock(t, time.Now())
	const n = 20000
	historyLog(t, n)
	s := readLogSnapshot()
	if len(s.blocks) < 10 {
		t.Fatalf("%d blocks, want the log in many", len(s.blocks))
	}
	var want []Record
	for _, packed := range s.blocks {
		c := packed.unpack()
		for i := range c.Rows {
			want = append(want, c.row(i).Record)
		}
	}
	var got []Record
	s.visit(time.Time{}, func(r Record) { got = append(got, r) })
	if len(got) != n || !reflect.DeepEqual(got, want) {
		t.Fatalf("visit read %d records, not the %d of the blocks decoded one by one", len(got), len(want))
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s.visit(time.Time{}, func(Record) {})
	runtime.ReadMemStats(&after)
	rows := int64(n) * int64(unsafe.Sizeof(packedRow{}))
	if allocated := int64(after.TotalAlloc - before.TotalAlloc); allocated > rows/4 {
		t.Fatalf("a visit of %d records allocated %d bytes; their rows alone are %d", n, allocated, rows)
	}
}
