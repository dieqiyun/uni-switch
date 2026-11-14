//go:build !windows

package proc

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// rss reads the command's resident memory with t.mu let go first: on
// macOS it asks ps, which Wait and Kill don't wait behind.
func (t *Tree) rss(cmd *exec.Cmd) (int64, bool) {
	pid := cmd.Process.Pid
	t.mu.Unlock()
	return pidRSS(pid)
}

// pidRSS is pid's resident memory: Linux's /proc/<pid>/status VmRSS, ps
// elsewhere. Both say it in KiB.
func pidRSS(pid int) (int64, bool) {
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "VmRSS:" {
				return parseKiB(f[1])
			}
		}
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	return parseKiB(strings.TrimSpace(string(out)))
}

func parseKiB(s string) (int64, bool) {
	kb, err := strconv.ParseInt(s, 10, 64)
	if err != nil || kb <= 0 {
		return 0, false
	}
	return kb << 10, true
}
