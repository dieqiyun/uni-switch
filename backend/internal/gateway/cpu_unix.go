//go:build unix

package gateway

import (
	"syscall"
	"time"
)

// processCPU is the user and system time this process used since it
// started, and whether the system said.
func processCPU() (time.Duration, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), true
}
