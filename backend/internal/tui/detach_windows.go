package tui

import "os/exec"

// detach: uni-switch on Windows never starts a serve of its own here
// (keepGateway is for Linux).
func detach(*exec.Cmd) {}
