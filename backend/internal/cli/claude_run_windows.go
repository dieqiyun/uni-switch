package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// claudeRunOS starts claude and waits for it, its exit code uni-switch's, as
// agyExecOS does. Ctrl+C is claude's to handle, so uni-switch lets it pass.
func claudeRunOS(bin string, args, env []string) error {
	cmd := proc.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return exitError{code: ee.ExitCode()}
	}
	return err
}
