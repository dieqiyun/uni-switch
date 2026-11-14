package proc

import "os/exec"

// RSS is the memory the tree holds resident, in bytes: its command's on
// Unix, every process of its job on Windows, where an npm install's
// claude.cmd is a cmd.exe with the node under it. False when it can't be
// read, or the command was waited for: its pid may be another process's
// by then.
func (t *Tree) RSS() (int64, bool) {
	t.mu.Lock()
	if t.done || t.cmd.Process == nil {
		t.mu.Unlock()
		return 0, false
	}
	return t.rss(t.cmd) // unlocks t.mu
}

// MemTotal is this computer's physical memory, in bytes; 0 when not known.
func MemTotal() int64 { return memTotal() }

// PeakRSS is the most the command held resident, in bytes, once it was
// waited for; 0 where the system doesn't say (Windows).
func PeakRSS(cmd *exec.Cmd) int64 {
	if cmd.ProcessState == nil {
		return 0
	}
	return peakRSS(cmd.ProcessState)
}
