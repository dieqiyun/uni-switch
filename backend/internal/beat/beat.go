// Package beat wakes the loops that look at something every so often at
// one time: each waits for the next multiple of its interval on the clock,
// so a 5s loop, a 15s one and a 30s one wake together. An idle uni-switch woke
// apart for each of them, a few threads every time (#3185). Timers due at
// one instant are run by one wakeup, so the loops need no timer in common.
package beat

import "time"

// Next fires at the next multiple of every on the clock, after now.
func Next(every time.Duration) <-chan time.Time {
	if every <= 0 {
		every = time.Millisecond
	}
	return time.After(time.Until(after(time.Now(), every)))
}

// after is the first multiple of every on the clock after now.
func after(now time.Time, every time.Duration) time.Time {
	return now.Truncate(every).Add(every)
}
