//go:build !darwin || !cgo

package proc

import (
	"context"
	"os/exec"
)

func combinedOutputOwn(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
	return cmd.CombinedOutput()
}

// Responsible is the process macOS holds responsible for pid, 0 when that
// can't be told (here, always).
func Responsible(pid int) int { return 0 }
