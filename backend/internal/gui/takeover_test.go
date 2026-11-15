package gui

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// A uni-switch that found the gateway taken serves it once the one that had it
// is gone (one left running from before an update, a uni-switch serve).
func TestGatewayTakenOver(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_ADDR", ln.Addr().String())
	other := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"uni-switch"}`))
	})}
	go other.Serve(ln)
	defer other.Close()
	// the watch and the gateway it takes up end with the test, before its
	// HOME and UNI_SWITCH_ADDR are put back
	ctx, stop := context.WithCancel(context.Background())
	watched := make(chan struct{})
	t.Cleanup(func() {
		stop()
		<-watched
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		if served.Load() != nil && servedRun.stop != nil {
			servedRun.stop()
			<-servedRun.done
		}
		served.Store(nil)
	})
	go func() {
		defer close(watched)
		watchGateway(ctx, 50*time.Millisecond)
	}()
	if serveGateway() != nil {
		t.Fatal("served over another uni-switch")
	}
	time.Sleep(200 * time.Millisecond)
	if served.Load() != nil {
		t.Fatal("took the gateway while the other had it")
	}
	other.Close()
	for deadline := time.Now().Add(5 * time.Second); served.Load() == nil || !gateway.Running(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the gateway wasn't taken up")
		}
	}
}

// A uni-switch that holds the gateway's port but doesn't answer in time (the
// service busy on a small server, or starting) still has it: uni-switch web
// beside it doesn't listen a second time, which only failed with "bind:
// address already in use" (K.Liu on Discord), and leaves the port to it.
func TestGatewayNotServedBesideASlowOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_ADDR", ln.Addr().String())
	slow := make(chan struct{})
	other := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-slow
		w.Write([]byte(`{"name":"uni-switch"}`))
	})}
	go other.Serve(ln)
	defer other.Close()
	defer close(slow)
	gw := serveGateway()
	t.Cleanup(func() {
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		if served.Load() != nil && servedRun.stop != nil {
			servedRun.stop()
			<-servedRun.done
		}
		served.Store(nil)
		takenSaid.Store(false)
	})
	if gw != nil {
		t.Fatal("listened for the gateway on a port another uni-switch holds")
	}
}
