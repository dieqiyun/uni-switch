package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ccStream plays Command Code's Chat API at its own host, which
// saysItsEnd reads the reply's end by: frames as a stream for the model,
// and an empty catalog for anything else that comes asking. Anything not
// going to that host goes on to its own transport, so the other members
// of a group stay on their httptest servers.
type ccStream struct {
	frames string
	next   http.RoundTripper
}

func (s ccStream) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "api.commandcode.ai" {
		return s.next.RoundTrip(r)
	}
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
	}
	if !strings.HasSuffix(r.URL.Path, "chat/completions") {
		return &http.Response{StatusCode: 200, Status: "200 OK",
			Header: http.Header{"Content-Type": {"application/json"}},
			Body:   io.NopCloser(strings.NewReader(`{"object":"list","data":[]}`)), Request: r}, nil
	}
	return &http.Response{StatusCode: 200, Status: "200 OK",
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(s.frames)), Request: r}, nil
}

// esp32gallery on #2471: a Chat reply of Command Code's ends with a
// finish_reason or not at all — 665 requests in a day, 624 ended with
// one, and the 20 that didn't were turns its slow starts lost to a
// killed worker, every ledger row a 200 with no stop and no error while
// the client read "Stream ended without finish_reason". Its API is one
// whose every whole reply says its end, as Gemini's OpenAI-compatible
// one is (saysItsEnd): a stream of its that stops short of a
// finish_reason is a reply cut short, said so in the stream, not a turn
// that ended as if whole.
func TestCommandCodeRelayEndedShort(t *testing.T) {
	fresh(t)
	half := sse(`data: {"id":"c","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`data: {"id":"c","choices":[{"index":0,"delta":{"content":"half said"}}]}`)
	s := New()
	s.client = &http.Client{Transport: ccStream{frames: half, next: http.DefaultTransport}}
	if err := provider.Save(provider.Provider{ID: "cc", Name: "Command Code", Key: "k",
		Models: []string{"m"}, Chat: "https://api.commandcode.ai/v1"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"cc/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	code, body := rec.Code, rec.Body.String()
	if code != 200 || !strings.Contains(body, "half said") {
		t.Fatalf("%d %s", code, body)
	}
	if !strings.Contains(body, "ended before it was complete") {
		t.Errorf("a cut Command Code reply ended as if whole: %s", body)
	}
	if strings.Contains(body, `"finish_reason"`) {
		t.Errorf("a cut reply given a finish_reason: %s", body)
	}
}

// A stream of another vendor's may end whole with neither a
// finish_reason nor [DONE]: it goes as it came, as it did.
func TestAnotherChatStreamWithoutAFinishReasonStaysAsItCame(t *testing.T) {
	fresh(t)
	half := sse(`data: {"id":"c","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`data: {"id":"c","choices":[{"index":0,"delta":{"content":"half said"}}]}`)
	a := &scripted{replies: []reply{{200, "text/event-stream", half}}}
	scriptedOn(t, "a", provider.Chat, a)
	s := New()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	code, body := rec.Code, rec.Body.String()
	if code != 200 || !strings.Contains(body, "half said") || strings.Contains(body, "ended before") {
		t.Fatalf("%d %s", code, body)
	}
}

// A whole reply — its finish_reason come, [DONE] or not — goes as it
// came, never a second end added to it.
func TestCommandCodeRelayWholeStaysWhole(t *testing.T) {
	fresh(t)
	whole := sse(`data: {"id":"c","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`data: {"id":"c","choices":[{"index":0,"delta":{"content":"all said"}}]}`,
		`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	for _, frames := range []string{whole, whole + "data: [DONE]\n\n"} {
		s := New()
		s.client = &http.Client{Transport: ccStream{frames: frames, next: http.DefaultTransport}}
		if err := provider.Save(provider.Provider{ID: "cc", Name: "Command Code", Key: "k",
			Models: []string{"m"}, Chat: "https://api.commandcode.ai/v1"}); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"cc/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
		code, body := rec.Code, rec.Body.String()
		if code != 200 || !strings.Contains(body, "all said") || strings.Contains(body, "ended before") || strings.Count(body, `"finish_reason"`) != 1 {
			t.Fatalf("%d %s", code, body)
		}
	}
}
