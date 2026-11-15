package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The ChatGPT backend saying a Codex account is out of its allowance, in
// each shape it says it: a 429 body, and a stream that fails with it.
var codexSpent = []struct {
	name   string
	refuse func(w http.ResponseWriter)
}{
	{"429 json", func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"plus","resets_in_seconds":7200}}`)
	}},
	{"429 bare", func(w http.ResponseWriter) {
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"type":"usage_limit_reached","message":"You've hit your usage limit. Upgrade to Pro, or try again at 9:34 PM.","plan_type":"plus","resets_at":4102444800}}`)
	}},
	{"response.failed usage_limit_reached", func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`event: response.created`+"\n"+`data: {"type":"response.created","sequence_number":0,"response":{"id":"r0","status":"in_progress"}}`,
			`event: codex.rate_limits`+"\n"+`data: {"type":"codex.rate_limits","plan_type":"plus","rate_limits":{"primary":{"used_percent":100.0,"window_minutes":300,"reset_after_seconds":7200}}}`,
			`event: response.failed`+"\n"+`data: {"type":"response.failed","sequence_number":1,"response":{"id":"r0","status":"failed","error":{"code":"usage_limit_reached","message":"You've hit your usage limit. Upgrade to Pro, or try again in 2 hours."}}}`))
	}},
	{"response.failed rate_limit_exceeded", func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r0","status":"in_progress"}}`,
			`event: response.failed`+"\n"+`data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"rate_limit_exceeded","message":"Rate limit reached for gpt-5.5 on tokens per min. Please try again in 11.054s."}}}`))
	}},
	{"stream without content type", func(w http.ResponseWriter) {
		// the ChatGPT backend streams without saying so
		io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"r0","status":"in_progress"}}`,
			`data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"usage_limit_reached","message":"You've hit your usage limit."}}}`))
	}},
	{"error event", func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r0"}}`,
			`event: error`+"\n"+`data: {"type":"error","error":{"type":"usage_limit_reached","code":"usage_limit_reached","message":"The usage limit has been reached","resets_in_seconds":7200}}`))
	}},
	{"usage_not_included", func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"type":"usage_not_included","message":"To use Codex with your ChatGPT plan, upgrade to Plus."}}`)
	}},
}

var grokAnswer = sse(`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	`data: [DONE]`)

// codexOn is Codex, signed in to ChatGPT, asking s at path.
func codexOn(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// usedUpShapes runs for the shapes of codexSpent saying the allowance is
// gone, rather than a moment's rate limit or a plan without Codex.
func usedUpShapes(t *testing.T, run func(t *testing.T, refuse func(w http.ResponseWriter))) {
	for _, x := range codexSpent {
		if x.name == "response.failed rate_limit_exceeded" || x.name == "usage_not_included" {
			continue
		}
		t.Run(x.name, func(t *testing.T) { run(t, x.refuse) })
	}
}
