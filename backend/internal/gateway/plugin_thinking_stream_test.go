package gateway

import (
	"io"
	"net/http"

	"sync"

	"time"
)

// heldThinker is an Anthropic vendor that says its first thought and then
// says nothing more until the agent has heard it (heard closed), or until
// giveUp passes. A gateway that holds the thinking until the reply ends
// lets the agent hear it only after giveUp, all at once.
type heldThinker struct {
	ct     []string // the reply's Content-Type: none when empty
	heard  chan struct{}
	giveUp time.Duration
	mu     sync.Mutex
	waited bool // the vendor gave up waiting: nothing reached the agent
}

func (v *heldThinker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	w.Header()["Content-Type"] = v.ct
	f := w.(http.Flusher)
	say := func(evs ...string) {
		for _, ev := range evs {
			io.WriteString(w, ev+"\n\n")
		}
		f.Flush()
	}
	say(glmThinksSlowly[0], glmThinksSlowly[1], glmThinksSlowly[2])
	select {
	case <-v.heard:
	case <-time.After(v.giveUp):
		v.mu.Lock()
		v.waited = true
		v.mu.Unlock()
	case <-r.Context().Done():
		return
	}
	say(glmThinksSlowly[3:]...)
}
