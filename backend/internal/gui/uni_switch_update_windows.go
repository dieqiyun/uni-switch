package gui

import (
	"os/exec"
	"syscall"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

func windowsInstallerCommand(path, directory string) *exec.Cmd {
	cmd := proc.Command(path)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// NSIS parses GetCommandLine itself and recognizes only a literal " /D=".
	// Go's normal argument quoting hides that switch when the path has spaces.
	// Quote only the executable; /D consumes the remaining unquoted directory.
	cmd.SysProcAttr.CmdLine = syscall.EscapeArg(path) + " /D=" + directory
	return cmd
}

func startWindowsInstaller(path, directory string) error {
	cmd := windowsInstallerCommand(path, directory)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
