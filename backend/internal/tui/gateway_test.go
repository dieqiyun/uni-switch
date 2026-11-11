package tui

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal(what)
}

// With no uni-switch serving, the terminal serves the gateway while it is open
// and lets go as it quits (gnayiab on X: ECONNREFUSED in WSL with only
// uni-switch tui).
func TestTUIServesTheGatewayWhenNoneDoes(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", freeAddr(t))
	if gateway.Running() {
		t.Fatal("a gateway answers before the terminal started")
	}
	ctx, stop := context.WithCancel(context.Background())
	wait := serveGateway(ctx)
	waitFor(t, "no gateway while the terminal is open", gateway.Running)
	if !gw.here.Load() {
		t.Fatal("not marked as served here")
	}
	if s := gatewayStatus(); !strings.Contains(s, gateway.URL()) || !strings.Contains(s, "served while uni-switch tui is open") {
		t.Fatalf("status %q", s)
	}
	stop()
	wait()
	waitFor(t, "the gateway still answers after the terminal quit", func() bool { return !gateway.Running() })
	if gw.here.Load() || gatewayStatus() != "" {
		t.Fatalf("still marked as served here: %q", gatewayStatus())
	}
}

// A uni-switch that has the gateway keeps it; the terminal takes it up once
// that one is gone.
func TestTUILeavesTheGatewayToAnotherAndTakesItUp(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", freeAddr(t))
	old := gatewayWatch
	gatewayWatch = 100 * time.Millisecond
	defer func() { gatewayWatch = old }()

	other, endOther := context.WithCancel(context.Background())
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		gateway.New().ListenAndServe(other)
	}()
	waitFor(t, "the other uni-switch's gateway never answered", gateway.Running)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	wait := serveGateway(ctx)
	time.Sleep(300 * time.Millisecond)
	if gw.here.Load() || gatewayStatus() != "" {
		t.Fatalf("took the gateway another uni-switch serves: %q", gatewayStatus())
	}

	endOther()
	<-otherDone
	waitFor(t, "the terminal didn't take the gateway up once the other was gone", func() bool {
		return gw.here.Load() && gateway.Running()
	})
	stop()
	wait()
}
