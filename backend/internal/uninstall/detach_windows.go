package uninstall

import (
	"os/exec"
	"syscall"
)

const (
	detachedProcess = 0x00000008
	newProcessGroup = 0x00000200
)

// detach starts cmd outside the app's console and process group, so it
// outlives the app.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= detachedProcess | newProcessGroup
}
