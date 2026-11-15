package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// openCodeBareBody is what OpenCode's gateway answered s-leeq's turn-start
// requests (upstream #3740): a 400 whose body names the model and
// nothing else — no error in it, no reason, no request id.
const openCodeBareBody = `{"model":"deepseek-v4.1-flash"}`

// openCodeServed is a turn the gateway answers.
const openCodeServed = `{"id":"gw-1","object":"chat.completion","model":"deepseek-v4.1-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`

// bareOpenCode answers the first refusals requests with the bare 400,
// then the rest with served; beyond that, replies[i], the last one after
// that. It counts what it was asked.
type bareOpenCode struct {
	mu       sync.Mutex
	refusals int
	replies  []bareReply
	asked    int
}

// bareReply is an answer of the gateway's, with the status it came as.
type bareReply struct {
	code int
	body string
}

func (b *bareOpenCode) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
	b.mu.Lock()
	i := b.asked
	b.asked++
	b.mu.Unlock()
	var x bareReply
	switch {
	case i < b.refusals:
		x = bareReply{400, openCodeBareBody}
	case i-b.refusals < len(b.replies):
		x = b.replies[i-b.refusals]
	default:
		x = b.replies[len(b.replies)-1]
	}
	return &http.Response{StatusCode: x.code, Status: statusText(x.code),
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   io.NopCloser(strings.NewReader(x.body)), Request: r}, nil
}

func statusText(code int) string {
	if code == 200 {
		return "200 OK"
	}
	return "400 Bad Request"
}

func (b *bareOpenCode) went() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.asked
}

func openCodeGoOf(t *testing.T, b http.RoundTripper) *Server {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "ocgo", Name: "OpenCode Go", Key: "k",
		Chat: "https://opencode.ai/zen/go/v1", Models: []string{"deepseek-v4.1-flash"}}); err != nil {
		t.Fatal(err)
	}
	pause := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = pause })
	s := New()
	s.client = &http.Client{Transport: b}
	return s
}

// postChat asks the gateway as s-leeq's client did, on /v1/chat/completions.
func postChat(t *testing.T, s *Server) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"ocgo/deepseek-v4.1-flash","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// The gateway refusing a turn's first request while the backend it names
// has nothing cached for it yet, then serving the same request (s-leeq's
// timeline: four 400s over 13s, a 200 of the same prompt 28s in) is the
// gateway's fault of a moment, not the request's: it is asked again, and
// the answer reaches the client. Read as the 400 it came as, nothing
// retries it: the caller's turn ended on it (#3740).
func TestOpenCodeGoBare400AskedAgain(t *testing.T) {
	b := &bareOpenCode{refusals: 1, replies: []bareReply{{200, openCodeServed}}}
	code, out := postChat(t, openCodeGoOf(t, b))
	if code != 200 || !strings.Contains(out, `"ok"`) || b.went() != 2 {
		t.Fatalf("%d %q, asked %d times", code, out, b.went())
	}
}

// A bare 400 the retries don't outlive is the gateway's refusal before
// the model was asked, not the request's own fault — said as the 502 it
// stands for, which a caller retries, not as a 400 that says the request
// was at fault, which nothing retries (#3740).
func TestOpenCodeGoBare400ToldAs502(t *testing.T) {
	b := &bareOpenCode{refusals: 99, replies: []bareReply{{200, openCodeServed}}}
	code, out := postChat(t, openCodeGoOf(t, b))
	if code != http.StatusBadGateway || !strings.Contains(out, "not an API reply") || strings.Contains(out, `"ok"`) {
		t.Fatalf("%d %q", code, out)
	}
	if b.went() < 3 { // asked again: the passing retries, then patience
		t.Fatalf("asked %d times", b.went())
	}
}

// A 400 that names anything — here the session OpenCode answers a request
// without — is an error, and goes to the caller as it came.
func TestOpenCodeGoError400Untouched(t *testing.T) {
	b := &bareOpenCode{replies: []bareReply{{400, openCodeMissing}}}
	code, out := postChat(t, openCodeGoOf(t, b))
	if code != http.StatusBadRequest || !strings.Contains(out, "x-opencode-session") {
		t.Fatalf("%d %q", code, out)
	}
	if b.went() != 1 {
		t.Fatalf("asked %d times", b.went())
	}
}
