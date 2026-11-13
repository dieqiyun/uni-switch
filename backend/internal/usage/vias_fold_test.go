package usage

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The whole log's model lists, counted on from the full blocks counted
// before, are the lists counted afresh: as calls come in over a block's
// edge, and when the log is written over.
func TestViasCountedOnAreCountedAfresh(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	writeFoldLog(t, now, 3*logBlockRows+100)
	check := func(step string) {
		t.Helper()
		got := cachedVias(time.Time{}).all
		want := buildVias(readLogSnapshot(), time.Time{}, provider.UsageRenames())
		if len(got.m) == 0 {
			t.Fatalf("%s: no lists", step)
		}
		if !reflect.DeepEqual(got.m, want.m) || !reflect.DeepEqual(got.first, want.first) {
			t.Fatalf("%s: counted on %d lists, afresh %d, or they differ", step, len(got.m), len(want.m))
		}
	}
	check("first")
	before := viaFolds
	for i := 0; i < logBlockRows+50; i++ {
		r := foldRecord(now, i)
		r.Time = now.Add(-time.Duration(i) * time.Second)
		r.Effort = []string{"", "high"}[i%2]
		Append(r)
		if i == 0 || i == 600 {
			check(fmt.Sprint("appended ", i+1))
		}
	}
	check("appended a block")
	if viaFolds-before < 2 {
		t.Fatalf("only %d lists counted on from what they had counted", viaFolds-before)
	}
	// a session first seen in the last block, then a model it never used
	Append(Record{Time: now, Agent: "codex", Session: "new", Provider: "openai", Model: "m3", Input: 5, Status: 200})
	Append(Record{Time: now, Agent: "codex", Session: "s0-0", Provider: "openai", Model: "m3", Input: 5, Status: 200})
	check("a new model")
	writeFoldLog(t, now, 2*logBlockRows+7)
	check("written over")
}
