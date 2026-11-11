package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"sync"
	"testing"
)

// cursorUp is an upstream for the fake plugin that answers every chat and
// keeps what each was asked: its model and service_tier.
type cursorUp struct {
	mu   sync.Mutex
	asks []map[string]any
}

func (u *cursorUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var q map[string]any
	b, _ := io.ReadAll(r.Body)
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.asks = append(u.asks, q)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`, `data: [DONE]`))
}

func (u *cursorUp) last(t *testing.T) map[string]any {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.asks) == 0 {
		t.Fatal("the plugin asked its upstream nothing")
	}
	return u.asks[len(u.asks)-1]
}
