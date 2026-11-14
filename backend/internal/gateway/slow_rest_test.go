package gateway

import (
	"io"
	"net/http"

	"strings"
	"sync"
	"sync/atomic"
)

// keyedSlow is slow to start on one key's one model while slow is set,
// and answers at once otherwise. It says who it was asked as, key/model.
type keyedSlow struct {
	key, model string
	slow       atomic.Bool
	arrived    chan struct{} // told of each slow request, when set
	mu         sync.Mutex
	asked      []string
}

func (k *keyedSlow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key, model := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), modelOf(body)
	k.mu.Lock()
	k.asked = append(k.asked, key+"/"+model)
	k.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	if key == k.key && model == k.model && k.slow.Load() {
		io.WriteString(w, `data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		if k.arrived != nil {
			k.arrived <- struct{}{}
		}
		<-r.Context().Done()
		return
	}
	io.WriteString(w, chatStreamOK)
}

func (k *keyedSlow) first() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.asked) == 0 {
		return ""
	}
	return k.asked[0]
}

func (k *keyedSlow) reset() {
	k.mu.Lock()
	k.asked = nil
	k.mu.Unlock()
}
