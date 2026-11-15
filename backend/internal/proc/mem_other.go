//go:build !darwin && !linux && !windows

package proc

func memTotal() int64 { return 0 }
