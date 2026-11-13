package proc

import (
	"os/exec"
	"time"
)

// probeGroup kills the probe and what it started, as CommandContext does:
// a job, as StartTree gives a command, is joined only once it has started,
// and Output starts it out of reach, so they are found by their parent at
// the kill. EndProbes ends it the same way when uni-switch exits.
func probeGroup(cmd *exec.Cmd, p *probe) {
	cmd.Cancel = func() error {
		p.kill()
		defer p.end()
		killDescendants(cmd.Process.Pid)
		return cmd.Process.Kill()
	}
}

// killWait is how long EndProbes waits, past a kill's start, for it to be
// over: the probe is killed alone, and nothing is signalled again.
func killWait() time.Duration { return time.Second }
