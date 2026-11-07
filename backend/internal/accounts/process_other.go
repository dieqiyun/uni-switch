//go:build !windows

package accounts

import "os/exec"

func hide(c *exec.Cmd) {}
