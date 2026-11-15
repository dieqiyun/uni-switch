//go:build !darwin

package agent

import (
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// listRunningNow lists the processes a pgrep -f pattern matches, with how
// long each has run and its command line, from ps.
func listRunningNow(pat string) []runningProc {
	out, _ := proc.Command("pgrep", "-f", pat).Output()
	pids := strings.Fields(string(out))
	if len(pids) == 0 {
		return nil
	}
	out, _ = proc.Command("ps", "-o", "etime=,command=", "-p", strings.Join(pids, ",")).Output()
	var ps []runningProc
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		et, cmd, _ := strings.Cut(strings.TrimSpace(l), " ")
		if up, ok := elapsed(et); ok {
			ps = append(ps, runningProc{up, strings.TrimSpace(cmd)})
		}
	}
	return ps
}
