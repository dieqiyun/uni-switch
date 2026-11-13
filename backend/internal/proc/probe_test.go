//go:build !windows

package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// probeScript is a CLI that is a script starting another program and
// waiting on it, as a version manager's wrapper does; it writes down the
// pid of what it started.
func probeScript(t *testing.T) (sh, pidFile string) {
	dir := t.TempDir()
	sh, pidFile = filepath.Join(dir, "cli"), filepath.Join(dir, "child.pid")
	testenv.Program(t, sh, "#!/bin/sh\nsleep 30 &\necho $! > "+pidFile+"\nwait\n")
	old := waitDelay
	waitDelay = 200 * time.Millisecond
	t.Cleanup(func() { waitDelay = old })
	return sh, pidFile
}

// runProbe runs the probe in the background; the channel gives what its
// run ended with.
func runProbe(cmd *exec.Cmd) chan error {
	done := make(chan error, 1)
	go func() {
		_, err := cmd.Output()
		done <- err
	}()
	return done
}

// childPid waits for the script to have started its child. A script just
// written can take seconds to start on a busy Mac, so the wait is long; a
// probe that ended before is said with its error.
func childPid(t *testing.T, pidFile string, done chan error) int {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	end := time.After(30 * time.Second)
	for {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
				return pid
			}
		}
		select {
		case err := <-done:
			t.Fatalf("the probe ended (%v) before the script started anything", err)
		case <-end:
			t.Fatal("the script started nothing in 30s")
		case <-tick.C:
		}
	}
}

// a probe whose context ends (its timeout) ends with what it started:
// killing the script alone left its child running. The context is ended
// once the script has started its child, not on a timer that a slow start
// could beat.
func TestProbeTimeoutEndsWhatItStarted(t *testing.T) {
	sh, pidFile := probeScript(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := runProbe(ProbeContext(ctx, sh))
	pid := childPid(t, pidFile, done)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the probe went on after its context ended")
	}
	if !gone(pid) {
		t.Fatalf("what the probe started (pid %d) outlived its context", pid)
	}
}

// a probe still asking when uni-switch exits is ended with it, rather than
// left to init
func TestEndProbes(t *testing.T) {
	sh, pidFile := probeScript(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := runProbe(ProbeContext(ctx, sh))
	pid := childPid(t, pidFile, done)
	start := time.Now()
	EndProbes()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the probe went on after EndProbes")
	}
	if !gone(pid) {
		t.Fatalf("what the probe started (pid %d) outlived EndProbes", pid)
	}
	if d := time.Since(start); d > endWait+time.Second {
		t.Fatalf("EndProbes took %v", d)
	}
}

// a probe that answered is forgotten once its context ends, so EndProbes
// doesn't wait on it
func TestProbeForgotten(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	if _, err := ProbeContext(ctx, "true").Output(); err != nil {
		t.Fatal(err)
	}
	cancel()
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		probes.Lock()
		n := len(probes.m)
		probes.Unlock()
		if n == 0 {
			return
		}
	}
	t.Fatal("a probe that answered is still listed")
}

// a process the group's kill missed — forked as the signal went out, as
// one of a launcher calling itself was (#1278) — is ended too: the group is
// signalled again while any of it lives. The first signal here misses all
// of it, as the race misses one.
func TestProbeKillEndsWhatTheSignalMissed(t *testing.T) {
	sh, pidFile := probeScript(t)
	old := signalGroup
	var mu sync.Mutex
	missed := false
	signalGroup = func(pgid int) error {
		mu.Lock()
		first := !missed
		missed = true
		mu.Unlock()
		if first {
			return nil // sent, and reached nobody
		}
		return old(pgid)
	}
	t.Cleanup(func() { signalGroup = old })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := runProbe(ProbeContext(ctx, sh))
	pid := childPid(t, pidFile, done)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the probe went on after its context ended")
	}
	if !gone(pid) {
		t.Fatalf("what the probe started (pid %d) outlived a kill that missed it once", pid)
	}
}

// missFor has the group's signal reach nobody for d from its first call, as
// signals racing a launcher's forks do, and puts the real one back after.
func missFor(t *testing.T, d time.Duration) {
	old := signalGroup
	var mu sync.Mutex
	var first time.Time
	signalGroup = func(pgid int) error {
		mu.Lock()
		if first.IsZero() {
			first = time.Now()
		}
		missing := time.Since(first) < d
		mu.Unlock()
		if missing {
			return nil // sent, and reached nobody
		}
		return old(pgid)
	}
	t.Cleanup(func() { signalGroup = old })
}

// uni-switch exiting right after EndProbes leaves nothing of a probe running,
// even when the kill missed some of it at first (JJ-Lin's case on #1278):
// EndProbes returned after the first signal, the exit ended the loop that
// signals the group again, and the missed child lived on, adopted by init.
// The exit is a real one: the test runs itself as a uni-switch that ends its
// probes and exits at once.
func TestEndProbesThenExitLeavesNothing(t *testing.T) {
	if os.Getenv("UNI_SWITCH_PROC_EXIT_HELPER") == "1" {
		missFor(t, 300*time.Millisecond)
		sh, pidFile := probeScript(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		done := runProbe(ProbeContext(ctx, sh))
		pid := childPid(t, pidFile, done)
		os.Stdout.WriteString("child " + strconv.Itoa(pid) + "\n")
		EndProbes()
		os.Exit(0)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestEndProbesThenExitLeavesNothing$")
	cmd.Env = append(os.Environ(), "UNI_SWITCH_PROC_EXIT_HELPER=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the exiting uni-switch: %v\n%s", err, out)
	}
	var pid int
	for _, l := range strings.Split(string(out), "\n") {
		if s, ok := strings.CutPrefix(l, "child "); ok {
			pid, _ = strconv.Atoi(s)
		}
	}
	if pid == 0 {
		t.Fatalf("the exiting uni-switch named no child:\n%s", out)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if !gone(pid) {
		t.Fatalf("what the probe started (pid %d) outlived uni-switch's exit after EndProbes", pid)
	}
}
