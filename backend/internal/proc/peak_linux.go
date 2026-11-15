package proc

import (
	"os"
	"syscall"
)

// peakRSS is the rusage's ru_maxrss, which Linux says in KiB.
func peakRSS(s *os.ProcessState) int64 {
	if ru, ok := s.SysUsage().(*syscall.Rusage); ok {
		return ru.Maxrss << 10
	}
	return 0
}
