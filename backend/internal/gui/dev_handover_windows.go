//go:build dev && windows

package gui

import "os"

// No handover on Windows: build/dev.sh restarts the backend there.
var handoverSignal os.Signal
