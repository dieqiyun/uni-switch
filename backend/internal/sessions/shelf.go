package sessions

import (
	"sync"
	"time"
)

// shelfIdle is how long a shelf keeps what it holds after its last use.
const shelfIdle = 10 * time.Second

// shelfHeld is the most a shelf holds, which bounds it: 4 MiB of 1 MiB line
// readers, 16 MiB of long lines, 4 zstd decoders (~3 MiB each).
const shelfHeld = 4

// shelf keeps up to shelfHeld buffers (or decoders) given back, for the next
// scan to take. A sync.Pool lets what it holds go at every second garbage
// collection, and a cold start collects about once a second: in its first
// 2 minutes (155 GCs) the pools were empty for 611 of the 1 MiB line
// readers, 205 long-line buffers and 281 zstd decoders scans asked for,
// ~1.9 GB allocated again. A shelf keeps them across collections while
// scans run, and lets them all go once none was taken or given back for
// shelfIdle, so an idle uni-switch holds none.
type shelf[T any] struct {
	idleFor time.Duration // shelfIdle when 0; set by tests

	mu    sync.Mutex
	held  []T
	used  time.Time
	timer *time.Timer
}

// take is the first held thing ok accepts, if any.
func (s *shelf[T]) take(ok func(T) bool) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = time.Now()
	for i, v := range s.held {
		if ok == nil || ok(v) {
			s.held = append(s.held[:i], s.held[i+1:]...)
			return v, true
		}
	}
	var zero T
	return zero, false
}

// give keeps v, in place of the first held thing worse says v beats when
// the shelf is full. It says false when v isn't kept (the shelf is full
// and v beats none), and gives back what v took the place of, if any.
func (s *shelf[T]) give(v T, worse func(held, v T) bool) (out T, kept, swapped bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = time.Now()
	if s.timer == nil {
		s.timer = time.AfterFunc(s.wait(), s.idle)
	}
	if len(s.held) < shelfHeld {
		s.held = append(s.held, v)
		return out, true, false
	}
	for i, h := range s.held {
		if worse != nil && worse(h, v) {
			s.held[i] = v
			return h, true, true
		}
	}
	return out, false, false
}

// idle lets go of everything held once the shelf has gone unused for
// shelfIdle, or checks again when it was used since.
func (s *shelf[T]) idle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if left := s.wait() - time.Since(s.used); left > 0 {
		s.timer.Reset(left)
		return
	}
	clear(s.held)
	s.held = s.held[:0]
	s.timer = nil
}

func (s *shelf[T]) wait() time.Duration {
	if s.idleFor > 0 {
		return s.idleFor
	}
	return shelfIdle
}

// size is how many things the shelf holds.
func (s *shelf[T]) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.held)
}
