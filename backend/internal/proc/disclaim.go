package proc

import (
	"context"
	"os/exec"
)

// On macOS, a privacy prompt (Desktop, Documents, Downloads, a removable
// volume…) names the process responsible for the one asking, and a child
// is the responsibility of whoever started it, down through every process
// it starts in turn. So a long-lived process uni-switch starts for an agent —
// Codex's app-server daemon, which every codex run in a terminal attaches
// to and works in the folder it was run in — asks in uni-switch's name: the
// first `codex` in ~/Desktop had macOS say uni-switch wants the Desktop
// folder (reformed9448 on Discord). CombinedOutputOwn starts such a
// command as its own responsible process instead, as a terminal's would
// be asked about under the terminal's name rather than uni-switch's.

// CombinedOutputOwn runs cmd, as made by Command or CommandContext with
// ctx, to its end and returns its output, stdout and stderr together, as
// cmd.CombinedOutput does; stdin is the null device. On macOS (built with
// cgo) the command, and what it starts, answer for themselves to macOS's
// privacy prompts rather than to uni-switch. Elsewhere it is
// cmd.CombinedOutput.
func CombinedOutputOwn(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	return combinedOutputOwn(ctx, cmd)
}
