package agent

import (
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// listRunningNow lists the processes a pgrep -f pattern matches, each with
// its command line from pgrep -lf and its start from the kernel. ps isn't
// run: on a Mac with a thousand processes and a busy CPU, ps -p over the
// 25 copies of claude a user keeps open took 1-2s, on every /api/state
// (#1962, #2341), where pgrep took 0.06s and a sysctl takes microseconds.
func listRunningNow(pat string) []runningProc {
	out, _ := proc.Command("pgrep", "-lf", pat).Output()
	var ps []runningProc
	now := time.Now()
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		id, cmd, ok := strings.Cut(strings.TrimSpace(l), " ")
		pid, err := strconv.Atoi(id)
		if !ok || err != nil {
			continue
		}
		kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil || kp.Proc.P_pid != int32(pid) {
			// gone since pgrep listed it
			continue
		}
		start := time.Unix(kp.Proc.P_starttime.Unix())
		if start.IsZero() || start.After(now) {
			continue
		}
		ps = append(ps, runningProc{now.Sub(start), strings.TrimSpace(cmd)})
	}
	return ps
}
