//go:build !darwin && !linux

package proc

import "os"

func peakRSS(*os.ProcessState) int64 { return 0 }
