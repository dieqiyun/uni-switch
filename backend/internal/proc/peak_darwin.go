package proc

import (
	"os"
	"syscall"
)

// peakRSS is the rusage's ru_maxrss, which macOS says in bytes.
func peakRSS(s *os.ProcessState) int64 {
	if ru, ok := s.SysUsage().(*syscall.Rusage); ok {
		return ru.Maxrss
	}
	return 0
}
