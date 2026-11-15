//go:build !windows

package tui

import (
	"os/exec"
	"syscall"
)

// detach keeps a started uni-switch out of this one's session, so it outlives
// it and its terminal.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
