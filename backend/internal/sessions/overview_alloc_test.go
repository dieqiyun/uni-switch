package sessions

import (
	"fmt"
	"runtime"
	"testing"
	"unsafe"
)

// The overview points to the sessions it sums up and copies only the few
// it shows: the Sessions page's poll copied every Summary four times, ~1.5
// GB in two minutes over 34k sessions while an agent worked (#2278).
func TestOverviewCopiesOnlyItsTop(t *testing.T) {
	st := Stats{From: "2026-10-01", To: "2026-10-28"}
	for i := range 5000 {
		st.Sessions = append(st.Sessions, Summary{Key: fmt.Sprint("claude:", i), Agent: "claude", ID: fmt.Sprint(i), Tokens: Tokens{Input: i, Output: 1}, Cost: float64(i), Active: int64(i), Days: []int{i % 28}})
	}
	st.Overview("", "", "", 8) // the maps' first growth
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	o := st.Overview("", "", "", 8)
	runtime.ReadMemStats(&after)
	if o.Count != 5000 || len(o.Top["tokens"]) != 8 || o.Top["tokens"][0].ID != "4999" || o.Top["cost"][7].ID != "4992" {
		t.Fatalf("count %d, top %d, first %q", o.Count, len(o.Top["tokens"]), o.Top["tokens"][0].ID)
	}
	all := uint64(len(st.Sessions)) * uint64(unsafe.Sizeof(Summary{}))
	if got := after.TotalAlloc - before.TotalAlloc; got > all/2 {
		t.Fatalf("the overview allocated %d bytes; its sessions are %d", got, all)
	}
}
