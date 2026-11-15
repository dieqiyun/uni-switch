//go:build !windows

package proc

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startSh runs script in a tree and gives the pid its first line names.
func startSh(t *testing.T, script string) (*Tree, int) {
	t.Helper()
	cmd := Command("/bin/sh", "-c", script)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("no pid from the script: %q", line)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return tree, pid
}

func gone(pid int) bool {
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

// Killing a command ends what it started, not only the command.
func TestTreeKillEndsChildren(t *testing.T) {
	tree, child := startSh(t, "sleep 600 >/dev/null 2>&1 & echo $!; wait")
	done := make(chan struct{})
	go func() { _ = tree.Wait(); close(done) }()
	tree.Kill()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the command outlived Kill")
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived Kill", child)
	}
	tree.Kill() // once waited for, a no-op
}

// A command that ends by itself takes what it left running with it.
func TestTreeWaitEndsWhatWasLeft(t *testing.T) {
	tree, child := startSh(t, "sleep 600 >/dev/null 2>&1 & echo $!")
	if err := tree.Wait(); err != nil {
		t.Fatal(err)
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived it", child)
	}
}

// Stop lets the command clean up before its tree ends: Claude Code removes
// the OAuth refresh lock it holds on SIGTERM, which a SIGKILL left behind
// (#2195). What it started still ends with it.
func TestTreeStopLetsItCleanUp(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "refresh.lock")
	tree, child := startSh(t, `sleep 600 >/dev/null 2>&1 & echo $!; trap 'rmdir "`+lock+`"; exit 0' TERM; mkdir "`+lock+`"; while :; do sleep 0.02; done`)
	for end := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(lock); err == nil {
			break
		} else if time.Now().After(end) {
			t.Fatal("the lock was never made")
		}
	}
	go func() { _ = tree.Wait() }()
	select {
	case <-tree.Stop(time.Minute):
	case <-time.After(10 * time.Second):
		t.Fatal("Stop never ended a command that exits on SIGTERM")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the command's lock is left after Stop: %v", err)
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived Stop", child)
	}
}

// A command that doesn't end when asked is killed, tree and all, once the
// grace has passed; a second Stop shares the first's.
func TestTreeStopKillsOneThatStays(t *testing.T) {
	tree, child := startSh(t, `sleep 600 >/dev/null 2>&1 & echo $!; trap '' TERM; while :; do sleep 0.02; done`)
	go func() { _ = tree.Wait() }()
	first := tree.Stop(50 * time.Millisecond)
	if second := tree.Stop(time.Hour); second != first {
		t.Fatal("a second Stop started a grace of its own")
	}
	select {
	case <-first:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop never killed a command that ignores SIGTERM")
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived Stop", child)
	}
	tree.Kill() // ended already: a no-op
}
