package gui

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// A restart that hands the gateway over waits only for the bridge's
// Claude Code turns, which can't go on in another process; one that
// doesn't waits for every request, as before (#577). The page is told
// which it is.
func TestRestartWaitsForWhatAHandoverCantCarry(t *testing.T) {
	at := time.Now().Add(-time.Second)
	b := gateway.Busy{Requests: 3, Tools: 1, Last: time.Now(), Bridge: 1, BridgeLast: at}
	if got := blocking(b, false); got != b {
		t.Fatalf("without a handover: %+v", got)
	}
	got := blocking(b, true)
	if got.Requests != 1 || got.Tools != 1 || !got.Last.Equal(at) {
		t.Fatalf("handing over, waits for %+v; want the bridge's turn and the parked tool", got)
	}
	if blocking(gateway.Busy{Requests: 4, Last: time.Now()}, true).Any() {
		t.Fatal("handing over, waits for requests that the old one finishes after it")
	}

	oldBusy, oldHands := gatewayBusy, handsOver
	t.Cleanup(func() { gatewayBusy, handsOver = oldBusy, oldHands })
	gatewayBusy = func() gateway.Busy { return gateway.Busy{Requests: 2} }
	u := &updater{state: "ready"}
	for _, hands := range []bool{false, true} {
		handsOver = func() bool { return hands }
		j := u.jsonIn("")
		if j.Handover != hands {
			t.Errorf("handover %v: the page reads %v", hands, j.Handover)
		}
		if (j.Busy != nil) == hands {
			t.Errorf("handover %v: the page shows busy %+v", hands, j.Busy)
		}
	}
}

// A restart hands over only from the app, on a Mac or Linux, with the
// gateway its own; otherwise it restarts as before.
func TestHandsOverOnlyWhereItCan(t *testing.T) {
	if handoverHost.Load() || served.Load() != nil {
		t.Skip("another test's gateway")
	}
	if handsOver() {
		t.Fatal("hands over outside the app")
	}
	oldExe := startExe
	t.Cleanup(func() { startExe = oldExe; handoverHost.Store(false) })
	startExe = "/apps/uni-switch"
	handoverHost.Store(true)
	if handsOver() {
		t.Fatal("hands over a gateway this uni-switch doesn't serve")
	}
	if beginHandover("", "/apps/uni-switch", false, "", false) || pending.Load() != nil {
		t.Fatal("began a handover it can't make")
	}
}
