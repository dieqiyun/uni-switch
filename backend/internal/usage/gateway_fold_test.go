package usage

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The gateway's sessions of all time, counted on from the full blocks
// counted before, are the sessions counted afresh, to the last bit of
// every cost: as calls come in over a block's edge, when a zone moves
// every day, and when the log is written over.
func TestGatewaySessionsCountedOnAreCountedAfresh(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	writeFoldLog(t, now, 3*logBlockRows+100)
	check := func(step string) {
		t.Helper()
		got := gatewayProjection().sessions
		want, _ := buildGatewaySessions(readLogSnapshot(), time.Time{}, nil, provider.UsageAlias())
		if len(got) == 0 {
			t.Fatalf("%s: no sessions", step)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: counted on %d sessions, afresh %d", step, len(got), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(got[i], want[i]) {
				t.Fatalf("%s: session %d counted on\n%+v\nafresh\n%+v", step, i, got[i], want[i])
			}
		}
	}
	check("first")
	before := gatewayFolds
	for i := 0; i < logBlockRows+50; i++ {
		r := foldRecord(now, i)
		r.Time = now.Add(-time.Duration(i) * time.Second)
		Append(r)
		if i == 0 || i == 600 {
			check(fmt.Sprint("appended ", i+1))
		}
	}
	check("appended a block")
	if gatewayFolds-before < 2 {
		t.Fatalf("only %d projections counted on from what they had counted", gatewayFolds-before)
	}
	Append(Record{Time: now, Agent: "codex", Session: "new", Provider: "openai", Model: "m3", Input: 5, Status: 200})
	Append(Record{Time: now.Add(-40 * 24 * time.Hour), Agent: "codex", Session: "s0-0", Provider: "openai", Model: "m3", Input: 5, Status: 200})
	check("a new session, and an older call")
	zone := time.Local
	time.Local = time.FixedZone("far", 13*3600)
	t.Cleanup(func() { time.Local = zone })
	check("another zone")
	time.Local = zone
	writeFoldLog(t, now, 2*logBlockRows+7)
	check("written over")
}
