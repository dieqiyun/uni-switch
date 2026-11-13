package proc

import (
	"errors"
	"time"
)

// ErrNoStart is a system that doesn't tell when a process started.
var ErrNoStart = errors.New("this system doesn't tell when a process started")

// Started is when process pid started, as the system recorded it. An
// error says it couldn't be read (gone, another user's, ErrNoStart), not
// that it started at any time.
func Started(pid int) (time.Time, error) { return startTime(pid) }
