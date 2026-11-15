package gui

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// A list, a manage page and a detail never read a gateway session's days,
// and copying every session's days for each poll of an open Sessions page
// was ~1.8 GB in 4 minutes over ~720k calls (#2776). Only the stats copy
// them, and they still count each one (TestGatewaySessionHistoryAndCalendar).
func TestGatewayListCopiesNoDays(t *testing.T) {
	g := usage.GatewaySession{Agent: "pi", ID: "s", Models: []usage.GatewayModel{{Model: "m"}}}
	for range 365 {
		g.Daily = append(g.Daily, usage.GatewayDayUsage{Date: "2026-10-19", Agent: "pi", Model: "m"})
	}
	if e := gatewayListed(g, sessions.GatewayTitle); len(e.Daily) != 0 || len(e.Models) != 1 {
		t.Fatalf("a listed session copied %d days and %d models, want 0 and 1", len(e.Daily), len(e.Models))
	}
}
