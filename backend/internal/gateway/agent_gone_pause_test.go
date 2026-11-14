package gateway

import (
	"io"
	"net/http"

	"sync/atomic"

	"time"
)

// unreachable answers every request as a relay does that can't reach its
// own upstream (heng91 on #1894: a relay behind a proxy that was off).
type unreachable struct{ n atomic.Int32 }

func (u *unreachable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	u.n.Add(1)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, `{"error":{"type":"api_error","message":"upstream connect error or disconnect/reset before headers"}}`)
}

// inPause tells whether the last route's tries number n and the last of
// them waits to be asked again, polled for up to within.
func inPause(s *Server, n int, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		s.trace.mu.Lock()
		ok := false
		if k := len(s.trace.routes); k > 0 {
			tries := s.trace.routes[k-1].Tries
			ok = len(tries) == n && tries[n-1].Again > 0
		}
		s.trace.mu.Unlock()
		if ok {
			return true
		}
	}
	return false
}
