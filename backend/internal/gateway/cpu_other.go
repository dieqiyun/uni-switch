//go:build !unix && !windows

package gateway

import "time"

// processCPU: this system doesn't say what the process used.
func processCPU() (time.Duration, bool) { return 0, false }
