package cli

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// HTTP transports and other long-lived workers keep reading the process
// clock between commands. A session fixture must not swap its local zone.
func TestSessionFixtureKeepsProcessClockStable(t *testing.T) {
	stop, done := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_ = time.Now()
				reads.Add(1)
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
	for reads.Load() == 0 {
		runtime.Gosched()
	}
	t.Run("session fixture", func(t *testing.T) { sessionsHome(t) })
}
