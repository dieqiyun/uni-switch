package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// streamingHold is a ResponseWriter that passes a reply on as it comes.
type streamingHold struct {
	h http.Header
	w io.Writer
}

func (s *streamingHold) Header() http.Header         { return s.h }
func (s *streamingHold) Write(b []byte) (int, error) { return s.w.Write(b) }
func (s *streamingHold) WriteHeader(int)             {}
func (s *streamingHold) Flush()                      {}

// Requests streaming at once each hold what their retries and
// continuations read, not the body that went upstream: that was held by
// the transport until the stream ended, and eight Claude Code requests
// of 4.7 MB translated to Chat held 152 MB mid-stream, 88 MB once it
// goes when it is answered (#3383). The heap is read once every
// request's first text has reached its agent: the upstream has written
// it before the gateway has the answer, and a request not answered yet
// still holds what it sent.
func TestStreamingRequestsLetTheirSentBodyGo(t *testing.T) {
	fresh(t)
	const n = 8
	answered := make(chan struct{}, n)
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"c","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, `data: {"id":"c","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "up", Name: "Up", Key: "k", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	var sb strings.Builder
	sb.WriteString(`{"model":"up/m1","max_tokens":100,"stream":true,"messages":[`)
	turn := strings.Repeat("<div>a & b</div> some text here ", 2000)
	for i := 0; i < 80; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		fmt.Fprintf(&sb, `{"role":%q,"content":%q}`, role, turn)
	}
	sb.WriteString(`]}`)
	body := sb.String()
	h := s.Handler()
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	before := ms.HeapAlloc
	var wg sync.WaitGroup
	defer wg.Wait()
	once := sync.OnceFunc(func() { close(release) })
	defer once()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pr, pw := io.Pipe()
			go firstText(t, pr, answered)
			h.ServeHTTP(&streamingHold{h: http.Header{}, w: pw}, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
			pw.Close()
		}()
	}
	for i := 0; i < n; i++ {
		<-answered
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	held := int64(ms.HeapAlloc) - int64(before)
	once()
	// the body as it came, for another member, and as read, for a
	// continuation: about twice the body each, the sent one 4 times
	if each := held / n; each > 3*int64(len(body)) {
		t.Errorf("%d streaming requests of %d MB hold %d MB, %.1f times the body each", n, len(body)>>20, held>>20, float64(each)/float64(len(body)))
	}
}

// firstText reads a stream as its agent does and says on answered when
// the reply's first text has come, the gateway's answer from the
// upstream with it; then it reads the rest.
func firstText(t *testing.T, r io.Reader, answered chan<- struct{}) {
	var seen []byte
	buf := make([]byte, 4096)
	for {
		k, err := r.Read(buf)
		seen = append(seen, buf[:k]...)
		if bytes.Contains(seen, []byte(`"text":"hi"`)) {
			break
		}
		if err != nil {
			t.Errorf("the stream ended before its first text: %q", seen)
			break
		}
	}
	answered <- struct{}{}
	io.Copy(io.Discard, r)
}
