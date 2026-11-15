package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// slowStart streams the role chunk at once and its text only after wait,
// or never when the request goes first.
type slowStart struct {
	wait time.Duration
	n    int
}

func (s *slowStart) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	s.n++
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	io.WriteString(w, `data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant"}}]}`+"\n\n")
	w.(http.Flusher).Flush()
	select {
	case <-r.Context().Done():
		return
	case <-time.After(s.wait):
	}
	io.WriteString(w, `data: {"id":"s","choices":[{"index":0,"delta":{"content":"slow answer"},"finish_reason":"stop"}]}`+"\n\n"+"data: [DONE]\n\n")
}

const chatStreamOK = `data: {"id":"b","choices":[{"index":0,"delta":{"role":"assistant","content":"from b"},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"

func slowOn(t *testing.T, id string, h http.Handler) {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
}
