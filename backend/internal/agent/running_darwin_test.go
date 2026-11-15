package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// /api/state asks StaleCopies for Claude and Codex on every call, and ps
// -p over a user's 25 copies of claude took 1-2s on a busy Mac, so every
// page switch and every save waited on it (#1962, #2341). listRunning
// finds a copy, its command line and its age without ps: a ps that only
// fails here is not run.
func TestRunningNeedsNoPs(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "ps"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cmd := proc.Command("sleep", "314.159")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	started := time.Now()

	var got []runningProc
	for range 50 {
		if got = listRunning(`sleep 314\.159`); len(got) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("found %d copies of sleep 314.159, want 1: %+v", len(got), got)
	}
	p := got[0]
	if !strings.Contains(p.cmd, "sleep 314.159") {
		t.Errorf("command line %q, want sleep 314.159", p.cmd)
	}
	// the kernel's start has whole seconds here, and the test may be slow
	if lim := time.Since(started) + 2*time.Second; p.up < 0 || p.up > lim {
		t.Errorf("up %v, want between 0 and %v", p.up, lim)
	}
}
