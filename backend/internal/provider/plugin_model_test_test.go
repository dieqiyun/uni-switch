package provider

import (
	"fmt"
	"net/http"

	"sync"
)

// answers is a vendor that streams one word on chat completions, and
// keeps the X-Uni-Switch-Fallback and X-Saw-Fallback each request came with.
type answers struct {
	mu   sync.Mutex
	seen []string
}

func (a *answers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.seen = append(a.seen, fmt.Sprintf("fallback=%q saw=%q", r.Header.Get(FallbackHeader), r.Header.Get("X-Saw-Fallback")))
	a.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
}
