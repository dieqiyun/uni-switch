//go:build !windows

package uninstall

import (
	"os/exec"
	"syscall"
)

// detach puts cmd in a session of its own: launchd ends the process
// group of an app that exits, which would take the files' removal with it.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
