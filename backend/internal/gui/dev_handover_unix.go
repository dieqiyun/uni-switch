//go:build dev && !windows

package gui

import (
	"os"
	"syscall"
)

// handoverSignal tells a dev backend its successor listens: USR2, so an
// INT or TERM still ends it at once.
var handoverSignal os.Signal = syscall.SIGUSR2
