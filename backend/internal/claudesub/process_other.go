//go:build !windows

package claudesub

import "os/exec"

func hide(c *exec.Cmd) {}
