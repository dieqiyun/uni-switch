package gateway

import (
	"io"
	"net/http"

	"sync"
)

// failsNext is an Anthropic Messages upstream that answers its next fails
// requests with status (an overload's 503 or a rate limit's 429), then as
// cacheWriter does, writing the whole prompt to its cache.
type failsNext struct {
	cacheWriter
	mu     sync.Mutex
	fails  int
	status int
	asked  int
}

func (f *failsNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.asked++
	fail := f.fails > 0
	if fail {
		f.fails--
	}
	status := f.status
	f.mu.Unlock()
	if !fail {
		f.cacheWriter.ServeHTTP(w, r)
		return
	}
	io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusTooManyRequests {
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
		return
	}
	io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
}

func (f *failsNext) failNext(n, status int) {
	f.mu.Lock()
	f.fails, f.status, f.asked = n, status, 0
	f.mu.Unlock()
}

func (f *failsNext) askedTimes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}
