package codexrelay

import (
	"os/exec"
	"syscall"
)

const (
	synchronize  = 0x00100000
	waitTimeout  = 0x00000102
	detachedProc = 0x00000008
	newProcGroup = 0x00000200
)

// alive reports whether pid is still running.
func alive(pid int) bool {
	h, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	ev, _ := syscall.WaitForSingleObject(h, 0)
	return ev == waitTimeout
}

// detach starts the relay outside uni-switch's console, so it outlives it.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= detachedProc | newProcGroup
}
