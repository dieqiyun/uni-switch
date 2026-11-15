package awake

import (
	"os/exec"
	"syscall"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// takeHold runs systemd-inhibit holding idle and sleep off around a sleep
// that never ends; the inhibitor goes with the process, which goes with
// uni-switch (Pdeathsig) should uni-switch end without letting go. The idle
// inhibitor is all logind has for the display as well, so display asks for
// nothing more: whether the screen stays on is the desktop's to say.
func takeHold(display bool) (func(), error) {
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return nil, err
	}
	cmd := proc.Command(path, "--what=idle:sleep", "--who=uni-switch", "--why=Agents are working through uni-switch", "--mode=block", "sleep", "infinity")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	}, nil
}
