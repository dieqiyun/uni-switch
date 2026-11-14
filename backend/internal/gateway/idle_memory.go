package gateway

import (
	"runtime/debug"
	"sync"
	"time"
)

// A request's body is held several times over while it is served (as
// read, masked, captured for the log), and Go keeps the pages it freed
// after: a tray uni-switch with ~60 requests an hour sat at 725 MB of private
// working set after a collection, its heap holding 1088 MB of which 315 MB
// was live (#3472). Go's scavenger hands such pages back only slowly, and
// not while the next collection's goal covers them. So once the gateway
// has had nothing in flight for idleRelease, what its requests left is
// handed back at once, as the warm-up does with what its reads left.

// idleRelease is how long the gateway rests before that.
const idleRelease = 30 * time.Second

// idleMemory is a Server's release of idle memory: a test sets its rest
// and counts its releases.
type idleMemory struct {
	mu      sync.Mutex
	due     *time.Timer   // the release to come, or nil
	rest    time.Duration // idleRelease when zero
	release func()        // debug.FreeOSMemory when nil
}

// stop calls off the release to come.
func (m *idleMemory) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.due != nil {
		m.due.Stop()
		m.due = nil
	}
}

func (m *idleMemory) restFor() time.Duration {
	if m.rest > 0 {
		return m.rest
	}
	return idleRelease
}

// armRelease has releaseIfResting run after d, unless it is due already.
func (s *Server) armRelease(d time.Duration) {
	s.idle.mu.Lock()
	defer s.idle.mu.Unlock()
	if s.idle.due == nil {
		s.idle.due = time.AfterFunc(d, s.releaseIfResting)
	}
}

// releaseIfResting hands memory back when nothing has been in flight for
// idleRelease. A request in flight now arms it again when it ends, and a
// rest not long enough yet waits out the rest of it.
func (s *Server) releaseIfResting() {
	s.idle.mu.Lock()
	s.idle.due = nil
	s.idle.mu.Unlock()
	if s.inFlight.Load() != 0 {
		return
	}
	if wait := s.idle.restFor() - time.Since(time.Unix(0, s.lastDone.Load())); wait > 0 {
		s.armRelease(wait)
		return
	}
	if s.idle.release != nil {
		s.idle.release()
		return
	}
	debug.FreeOSMemory()
}
