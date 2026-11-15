package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"

	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// decideUp is the fake plugin's vendor serving conversations and System
// One both: it keeps the path, model and sign-in of each request.
type decideUp struct {
	mu    sync.Mutex
	asked []string // "path model authorization"
}

func (u *decideUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var q struct{ Model string }
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.asked = append(u.asked, r.URL.Path+" "+q.Model+" "+r.Header.Get("Authorization"))
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/systemone":
		io.WriteString(w, `{"model":"`+q.Model+`","answers":{"levels":{"type":"noul","noul":0},"intent":{"type":"choice","choice":"bug","confidence":0.9}}}`)
	case "/v1/chat/completions":
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from the plugin"},"finish_reason":"stop"}]}`, `data: [DONE]`))
	default:
		http.NotFound(w, r)
	}
}

func (u *decideUp) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.asked)
}

// offered reports whether agents are offered the model id.
func offered(id string) bool {
	return slices.ContainsFunc(provider.Catalog(), func(e provider.Entry) bool { return e.ID == id })
}
