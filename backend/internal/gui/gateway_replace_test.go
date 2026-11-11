package gui

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// resetSkew forgets what the tests' fakes left of another uni-switch seen
// serving the gateway (set by gateway_skew_test.go).
var resetSkew = func() {}

// skewHome gives the test a home and a gateway port of its own, this
// uni-switch the version mine and the app's handover host, and stops the
// gateway it comes to serve when it ends.
func skewHome(t *testing.T, mine string) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))
	t.Setenv("UNI_SWITCH_ADDR", addr)
	was, wasHost := gateway.Version, handoverHost.Load()
	gateway.Version = mine
	handoverHost.Store(true)
	served.Store(nil)
	resetSkew()
	waits := []*time.Duration{&quitWait, &killWait, &startWait}
	olds := []time.Duration{quitWait, killWait, startWait}
	quitWait, killWait, startWait = 3*time.Second, 2*time.Second, 5*time.Second
	t.Cleanup(func() {
		gatewayMu.Lock()
		if served.Load() != nil && servedRun.stop != nil {
			servedRun.stop()
			<-servedRun.done
		}
		served.Store(nil)
		gatewayMu.Unlock()
		gateway.Version = was
		handoverHost.Store(wasHost)
		for i, w := range waits {
			*w = olds[i]
		}
		resetSkew()
	})
	return addr
}

// startFakeuni-switch runs the test binary, named uni-switch, as another uni-switch
// of version v holding the gateway's port, started as serves says; ended
// is closed once it has ended.
func startFakeUniSwitch(t *testing.T, addr, v, serves string) (ended chan struct{}) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "uni-switch"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	if runtime.GOOS == "windows" || os.Link(self, bin) != nil {
		b, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "-test.run=^$")
	cmd.Env = append(os.Environ(), "UNI_SWITCH_TEST_FAKE_GATEWAY="+addr, "UNI_SWITCH_TEST_FAKE_VERSION="+v, "UNI_SWITCH_TEST_FAKE_SERVES="+serves)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended = make(chan struct{})
	go func() { cmd.Wait(); close(ended) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-ended })
	for deadline := time.Now().Add(10 * time.Second); gateway.ServedBy().Version != v; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the fake uni-switch %s didn't answer", v)
		}
	}
	return ended
}

// matrix0487 on Discord: after an update the app kept the gateway an older
// copy of it served, said so nowhere but the Gateway page, and pi got 404
// for a model the newer app had written it. Now the app takes the gateway
// over from its older copy by itself, once that one has no request in
// flight: an agent's stream through it runs to its end, and then this one
// serves the gateway.
func TestAppReplacesItsOlderGatewayOnceIdle(t *testing.T) {
	addr := skewHome(t, "0.1.2500")
	ended := startFakeUniSwitch(t, addr, "0.1.2400", "app")

	// an agent's reply streaming through the older one
	lines := make(chan int, 1)
	go func() {
		res, err := http.Get("http://" + addr + "/stream")
		if err != nil {
			lines <- -1
			return
		}
		defer res.Body.Close()
		n := 0
		for sc := bufio.NewScanner(res.Body); sc.Scan(); {
			if strings.HasPrefix(sc.Text(), "line ") {
				n++
			}
		}
		lines <- n
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if res, err := http.Get("http://" + addr + "/v1/uni-switch/concurrency"); err == nil {
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if strings.Contains(string(b), `"requests":1,`) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the stream never started")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchGateway(ctx, 100*time.Millisecond)

	// while it streams, it is left serving
	select {
	case n := <-lines:
		if n != 20 {
			t.Fatalf("the stream through the older uni-switch was cut at %d lines of 20", n)
		}
	case <-ended:
		t.Fatal("the older uni-switch was quit with a request in flight")
	case <-time.After(15 * time.Second):
		t.Fatal("the stream never ended")
	}

	// then it is quit, and this one serves the gateway
	select {
	case <-ended:
	case <-time.After(15 * time.Second):
		t.Fatal("the older copy of the app still serves the gateway")
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if served.Load() != nil && gateway.ServedBy().Version == "0.1.2500" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not served here after the older one quit: %+v", gateway.ServedBy())
		}
	}
}
