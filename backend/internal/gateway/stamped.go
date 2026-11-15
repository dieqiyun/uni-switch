package gateway

import (
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// stampedFor is how long what a stamped keeps is used while the stamp
// stays the same: a list another uni-switch fetched, or an account another
// program signed in to, moves no stamp, and is seen this long after.
const stampedFor = time.Minute

// stamped keeps what is built from the providers' model lists, by key,
// until provider.StampNow moves or stampedFor has gone by. Each
// AutoVision (every agent's model list asks it, provider.Described) built
// every provider's list again, twice, and a probe of the gateway (info)
// the whole catalog to count it: 5.3 GB and 5.4 GB of a day's 36 GB of
// allocations (yg6888 on X, ~1 GB held). Two callers building one key at
// once each build it; none waits on another.
type stamped[K comparable, V any] struct {
	mu    sync.Mutex
	stamp provider.Stamp
	at    time.Time
	of    map[K]V
}

func (s *stamped[K, V]) get(k K, build func() V) V {
	st := provider.StampNow()
	s.mu.Lock()
	if s.stamp != st || time.Since(s.at) >= stampedFor {
		s.stamp, s.at, s.of = st, time.Now(), nil
	}
	if v, ok := s.of[k]; ok {
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()
	// built from what is there after st was taken: never older than st
	v := build()
	s.mu.Lock()
	if s.stamp == st {
		if s.of == nil {
			s.of = map[K]V{}
		}
		s.of[k] = v
	}
	s.mu.Unlock()
	return v
}
