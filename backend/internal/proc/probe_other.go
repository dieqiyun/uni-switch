//go:build !windows

package proc

import (
	"os/exec"
	"time"
)

// probeGroup has the probe lead a session, and its context ending kill the
// session's group: the script at its head and what that started. The probe
// counts as ended once none of the group is left, not once the first signal
// went out (#1278).
func probeGroup(cmd *exec.Cmd, p *probe) {
	ownGroup(cmd)
	cmd.Cancel = func() error {
		p.kill()
		var g group
		gone := g.kill(cmd)
		go func() { <-gone; p.end() }()
		return cmd.Process.Kill()
	}
}

// killWait is how long EndProbes waits, past a kill's start, for the group
// to be empty: as long as the kill goes on signalling it, and a moment more.
func killWait() time.Duration { return killFor + time.Second }
