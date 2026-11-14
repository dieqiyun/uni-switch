//go:build !windows

package gateway

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// Ending a Claude Code run lets it clean up first: one holding its OAuth
// refresh lock removes it as it exits on SIGTERM, and the SIGKILL uni-switch
// sent left ~/.claude/.oauth_refresh.lock for every later request to fail
// on (#2195, XIQIXIQIXIQI's test).
func TestClaudeAbortAllowsLockCleanup(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "refresh.lock")
	cmd := proc.Command("/bin/sh", "-c",
		`trap 'rmdir "$1"; exit 0' TERM; mkdir "$1"; echo ready; while :; do sleep 0.02; done`,
		"sh", lock)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	run := &subscriptionRun{cmd: cmd}
	if err := run.launch(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.tree.Kill(); <-run.exited })
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("not ready: %q %v", line, err)
	}
	<-run.abort()
	<-run.exited
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("Claude abort left refresh lock: %v", err)
	}
}
