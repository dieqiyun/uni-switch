package gateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// A request uni-switch's own MCP server makes of its client while it answers
// one of the client's (MCP Streamable HTTP): the POST is answered with an
// event stream instead of JSON, the request is one event on it, the client
// POSTs its answer, and the reply ends the stream. The hub asks so for an
// agent's roots (hubRoots).

type mcpExchangeKey struct{}

// mcpExchange is one POST's answer: canStream when the client takes an
// event stream back for it, streamed once the stream has begun.
type mcpExchange struct {
	w         http.ResponseWriter
	canStream bool
	mu        sync.Mutex
	streamed  bool
}

// send writes m as one event of the stream, beginning it.
func (x *mcpExchange) send(m any) {
	b, _ := json.Marshal(m)
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.streamed {
		h := x.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		x.w.WriteHeader(http.StatusOK)
		x.streamed = true
	}
	fmt.Fprintf(x.w, "event: message\ndata: %s\n\n", b)
	http.NewResponseController(x.w).Flush()
}

// clientAsked are the requests made of clients, by id, each waiting for its answer.
var clientAsked sync.Map // string(id) -> chan mcpMessage

// clientAnswered hands a client's answer to the request it is of; one of
// nothing asked is let be.
func clientAnswered(m mcpMessage) {
	if ch, ok := clientAsked.LoadAndDelete(string(m.ID)); ok {
		ch.(chan mcpMessage) <- m
	}
}

var errCantAsk = errors.New("the client takes no event stream back")

// askClient makes a request of the client whose call ctx is of, and is its
// answer, given within wait.
func askClient(ctx context.Context, method string, params any, wait time.Duration) (mcpMessage, error) {
	x, _ := ctx.Value(mcpExchangeKey{}).(*mcpExchange)
	if x == nil || !x.canStream {
		return mcpMessage{}, errCantAsk
	}
	id, _ := json.Marshal("uni-switch-" + rand.Text())
	ch := make(chan mcpMessage, 1)
	clientAsked.Store(string(id), ch)
	defer clientAsked.Delete(string(id))
	req := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method}
	if params != nil {
		req["params"] = params
	}
	x.send(req)
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case m := <-ch:
		return m, nil
	case <-t.C:
		return mcpMessage{}, fmt.Errorf("the client didn't answer %s in %s", method, wait)
	case <-ctx.Done():
		return mcpMessage{}, ctx.Err()
	}
}
