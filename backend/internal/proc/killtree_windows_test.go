package proc

import (
	"context"
	"os/exec"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// alive says pid is a process still running.
func alive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == 259 // STILL_ACTIVE
}

// The Store's WSL runs System32's wsl.exe, which starts Program Files'
// wsl.exe and waits on it. A listing past its timeout had only the first
// killed, and the second, waiting on a hung service, was left: dozens a day
// (#2341). A command's context ending kills what it started too. A ready
// parent and child test process stand in for the two wsl.exe.
func TestContextEndKillsWhatTheCommandStarted(t *testing.T) {
	for _, name := range []string{"CommandContext", "ProbeContext"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mk := CommandContext
			if name == "ProbeContext" {
				mk = ProbeContext
			}
			cmd, kids, done := startedTree(t, func(_ context.Context, name string, args ...string) *exec.Cmd {
				return mk(ctx, name, args...)
			})
			if !slices.Contains(descendants(uint32(cmd.Process.Pid)), kids[0]) {
				t.Fatal("the ready child is not listed under its parent")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("parent not ended 10s after its context")
			}
			for _, k := range kids {
				for i := 0; alive(k) && i < 40; i++ {
					time.Sleep(50 * time.Millisecond)
				}
				if alive(k) {
					t.Errorf("child process %d still runs after its parent's context ended", k)
				}
			}
		})
	}
}

// The Codex app's codex.exe app-server, ended by End so the app starts it
// again, left the MCP servers it ran (node.exe under npx) running with no
// parent, one set more for every restart (liam_8829, 10-27). End ends what
// pid started too, and pid still exits 0. Ready parent and child test
// processes stand in for codex.exe and an MCP server.
func TestEndEndsWhatItStarted(t *testing.T) {
	cmd, kids, done := startedTree(t, CommandContext)
	if err := End(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("parent ended with %v, want exit code 0", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("parent not ended 10s after End")
	}
	for _, k := range kids {
		for i := 0; alive(k) && i < 100; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		if alive(k) {
			t.Errorf("child process %d still runs after End", k)
		}
	}
}
