package proc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The parent reports its child's PID only after that child reports ready.
// It deliberately leaves cleanup to the caller: killing only the parent
// must leave the child alive so the tree-killing assertions detect it.
func TestProcessTreeHelper(t *testing.T) {
	mode := os.Getenv("UNI_SWITCH_PROC_TREE_HELPER")
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	if mode == "child" {
		fmt.Println("ready")
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(self, "-test.run=^TestProcessTreeHelper$")
	child.Env = append(os.Environ(), "UNI_SWITCH_PROC_TREE_HELPER=child")
	hide(child)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(child.Process.Pid)
	if err := os.WriteFile(os.Getenv("UNI_SWITCH_PROC_TREE_PID_FILE"), []byte(pid), 0o600); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("child readiness: %q, %v", line, err)
	}
	fmt.Println(pid)
	time.Sleep(2 * time.Minute)
	os.Exit(0)
}

func startedTree(t *testing.T, makeCommand func(context.Context, string, ...string) *exec.Cmd) (*exec.Cmd, []uint32, <-chan error) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	cmd := makeCommand(ctx, self, "-test.run=^TestProcessTreeHelper$")
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd.Env = append(os.Environ(), "UNI_SWITCH_PROC_TREE_HELPER=parent", "UNI_SWITCH_PROC_TREE_PID_FILE="+pidFile)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	var ownedChild *os.Process
	t.Cleanup(func() {
		// Every PID here comes from this run's fixture, never a process
		// selected by an installed application's executable or name.
		if ownedChild != nil {
			_ = ownedChild.Kill()
			_ = ownedChild.Release()
		} else if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(string(b)); err == nil {
				if path, err := executable(pid); err == nil && strings.EqualFold(path, self) {
					killAll([]uint32{uint32(pid)})
				}
			}
		}
		_ = cmd.Process.Kill()
		<-done
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	var child int
	select {
	case line := <-ready:
		child, err = strconv.Atoi(line)
		if err != nil || child <= 0 {
			t.Fatalf("parent did not report its ready child: %q", line)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("parent did not report child readiness")
	}
	if !alive(uint32(child)) {
		t.Fatal("the ready child already exited")
	}
	ownedChild, err = os.FindProcess(child)
	if err != nil {
		t.Fatal(err)
	}
	return cmd, []uint32{uint32(child)}, done
}
