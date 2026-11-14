package proc

import (
	"context"
	"os/exec"
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
// (#2341). A command's context ending kills what it started too. cmd.exe
// running ping stands in for the two wsl.exe.
func TestContextEndKillsWhatTheCommandStarted(t *testing.T) {
	for _, name := range []string{"CommandContext", "ProbeContext"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mk := CommandContext
			if name == "ProbeContext" {
				mk = ProbeContext
			}
			cmd := mk(ctx, "cmd.exe", "/c", "ping -n 60 127.0.0.1 >NUL")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { cmd.Wait(); close(done) }()
			var kids []uint32
			for range 100 {
				if kids = descendants(uint32(cmd.Process.Pid)); len(kids) > 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if len(kids) == 0 {
				t.Fatal("cmd.exe started no ping")
			}
			t.Cleanup(func() {
				for _, k := range kids {
					if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, k); err == nil {
						windows.TerminateProcess(h, 1)
						windows.CloseHandle(h)
					}
				}
			})
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("cmd.exe not ended 10s after its context")
			}
			for _, k := range kids {
				for i := 0; alive(k) && i < 40; i++ {
					time.Sleep(50 * time.Millisecond)
				}
				if alive(k) {
					t.Errorf("process %d that cmd.exe started still runs after its context ended", k)
				}
			}
		})
	}
}

// The Codex app's codex.exe app-server, ended by End so the app starts it
// again, left the MCP servers it ran (node.exe under npx) running with no
// parent, one set more for every restart (liam_8829, 10-27). End ends what
// pid started too, and pid still exits 0. cmd.exe running ping stands in
// for codex.exe and an MCP server.
func TestEndEndsWhatItStarted(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var kids []uint32
	for range 100 {
		if kids = descendants(uint32(cmd.Process.Pid)); len(kids) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(kids) == 0 {
		t.Fatal("cmd.exe started no ping")
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		killAll(kids)
	})
	if err := End(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cmd.exe ended with %v, want exit code 0", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cmd.exe not ended 10s after End")
	}
	for _, k := range kids {
		for i := 0; alive(k) && i < 100; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		if alive(k) {
			t.Errorf("process %d that cmd.exe started still runs after End", k)
		}
	}
}
