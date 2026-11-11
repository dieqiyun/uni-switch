package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"strings"
	"sync"
	"testing"
)

// tierUp is an upstream for the fake plugin that answers Chat and Responses
// and keeps the service_tier each request was sent with ("" for none).
type tierUp struct {
	mu    sync.Mutex
	tiers []any
}

func (u *tierUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var q map[string]any
	b, _ := io.ReadAll(r.Body)
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.tiers = append(u.tiers, q["service_tier"])
	u.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	if strings.HasSuffix(r.URL.Path, "/responses") {
		io.WriteString(w, sse(
			`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r1","object":"response","status":"in_progress","output":[]}}`,
			`event: response.output_item.done`+"\n"+`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}}`,
			`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r1","object":"response","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`))
		return
	}
	io.WriteString(w, sse(`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`, `data: [DONE]`))
}

func (u *tierUp) last(t *testing.T) any {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.tiers) == 0 {
		t.Fatal("the plugin asked its upstream nothing")
	}
	return u.tiers[len(u.tiers)-1]
}

// tiersLike compares "id=name " lists by id, and by name where want names
// one: ultrafast's name comes from Codex's own catalog when it has one.
func tiersLike(got, want string) bool {
	g, w := strings.Fields(got), strings.Fields(want)
	if len(g) != len(w) {
		return false
	}
	for i := range w {
		gi, gn, _ := strings.Cut(g[i], "=")
		wi, wn, _ := strings.Cut(w[i], "=")
		if gi != wi || wn != "" && gn != wn {
			return false
		}
	}
	return true
}
