//go:build !windows

package proc

import "os/exec"

// hide has nothing to do: only Windows gives a child a window of its own.
func hide(*exec.Cmd) {}

// batch has nothing to do: only Windows runs a script through cmd.exe.
func batch(*exec.Cmd) {}

// cancelTree has nothing to do: a Unix command that should take what it
// started with it is a probe, which leads a group of its own.
func cancelTree(*exec.Cmd) {}
