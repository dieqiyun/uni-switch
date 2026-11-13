package gui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A uni-switch in a container holding the gateway's port (leslie_luo on
// Discord: uni-switch v0.1.552 in OrbStack had 3425, held on the Mac by
// OrbStack Helper). The take-over names it as a container's, not as a
// program to quit, and leaves it running; and Settings' port, which was
// refused while "another uni-switch" served, moves this uni-switch's gateway to a
// port of its own. A uni-switch program of this computer's on the port still
// has its port set there. Run against copies of the test binary, on ports
// of the test's own.
func TestGatewayHeldByAContainer(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("APPDATA", filepath.Join(h, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(h, "AppData", "Local"))
	t.Setenv("UNI_SWITCH_ADDR", "")
	from := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(from)
	s := settings.Load()
	s.Port = from
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	was := gateway.Version
	gateway.Version = "0.1.630"
	defer func() { gateway.Version = was }()
	served.Store(nil)
	t.Cleanup(func() {
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		if served.Load() != nil && servedRun.stop != nil {
			servedRun.stop()
			<-servedRun.done
		}
		served.Store(nil)
	})

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	start := func(name string) (*exec.Cmd, chan struct{}) {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		bin := filepath.Join(dir, name)
		if err := os.Link(self, bin); err != nil {
			b, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, b, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(bin, "-test.run=^$")
		cmd.Env = append(os.Environ(), "UNI_SWITCH_TEST_FAKE_GATEWAY="+addr)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ended := make(chan struct{})
		go func() { cmd.Wait(); close(ended) }()
		t.Cleanup(func() { cmd.Process.Kill(); <-ended })
		for deadline := time.Now().Add(10 * time.Second); gateway.ServedBy().Version != "0.1.550"; time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s didn't answer", name)
			}
		}
		return cmd, ended
	}

	// a uni-switch of this computer's on the port: its port is set there
	local, localEnded := start("uni-switch")
	if _, err := setPort(freePort(t)); err == nil || settings.Load().Port != from {
		t.Fatalf("moved past a uni-switch of this computer's: %v", err)
	}
	local.Process.Kill()
	<-localEnded

	helper, helperEnded := start("OrbStack Helper")
	out := takeOverGateway()
	if out.OK || out.Reason != "container" || out.PID != helper.Process.Pid {
		t.Fatalf("a container's port: %+v", out)
	}
	select {
	case <-helperEnded:
		t.Fatal("ended the container's helper")
	case <-time.After(300 * time.Millisecond):
	}

	to := freePort(t)
	res, err := setPort(to)
	if err != nil || res.Port != to {
		t.Fatalf("the port couldn't be moved past the container: %+v %v", res, err)
	}
	for deadline := time.Now().Add(5 * time.Second); !answers(to); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the gateway didn't start on the new port")
		}
	}
	if g := providersState().Gateway; !g.Mine || !g.Running || g.Older {
		t.Fatalf("not served here on the new port: %+v", g)
	}
	select {
	case <-helperEnded:
		t.Fatal("the container's uni-switch was ended")
	default:
	}
}
