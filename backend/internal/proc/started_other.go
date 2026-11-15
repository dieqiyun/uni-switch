//go:build !darwin && !linux && !windows

package proc

import "time"

func startTime(int) (time.Time, error) { return time.Time{}, ErrNoStart }
