package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ruleUp is a Chat vendor that answers as its key, streaming when asked,
// and counts the prompt as 3000 tokens, 2500 of them read from its cache.
type ruleUp struct {
	mu    sync.Mutex
	key   string
	fail  int
	calls []string // "stream" or "json"
	seen  int      // requests that carried an image
	last  string   // the last request's body
}

func (u *ruleUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	stream := strings.Contains(string(b), `"stream":true`)
	u.mu.Lock()
	u.calls = append(u.calls, map[bool]string{true: "stream", false: "json"}[stream])
	u.last = string(b)
	if strings.Contains(string(b), "image_url") {
		u.seen++
	}
	fail := u.fail
	u.mu.Unlock()
	if fail != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(fail)
		io.WriteString(w, `{"error":{"message":"down"}}`)
		return
	}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from `+u.key+`"}}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":3000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2500}}}`,
			`data: [DONE]`,
		))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"from `+u.key+`"},"finish_reason":"stop"}],`+
		`"usage":{"prompt_tokens":3000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2500}}}`)
}

func (u *ruleUp) n() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.calls)
}

// chat builds a Chat conversation: the user's words, each after the first
// answered by a tool call and its result when tools is set.
func chat(first string, turns []string, toolsAfter int, extra string) string {
	var ms []string
	for i, w := range append([]string{first}, turns...) {
		if i > 0 {
			ms = append(ms, `{"role":"assistant","content":"done"}`)
		}
		ms = append(ms, `{"role":"user","content":`+quote(w)+`}`)
	}
	for i := range toolsAfter {
		id := fmt.Sprintf("c%d", i)
		ms = append(ms, `{"role":"assistant","content":null,"tool_calls":[{"id":"`+id+`","type":"function","function":{"name":"ls","arguments":"{}"}}]}`,
			`{"role":"tool","tool_call_id":"`+id+`","content":"file.txt"}`)
	}
	return `{"model":"group/r"` + extra + `,"messages":[` + strings.Join(ms, ",") + `]}`
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func lastRoute(s *Server) Route {
	s.trace.mu.Lock()
	defer s.trace.mu.Unlock()
	return *s.trace.routes[len(s.trace.routes)-1]
}

func long(tokens int) string { return strings.Repeat("abcd", tokens) }

// Claude Code puts a system message after its tool results: the turn
// still goes on.
func TestTurnInPastTrailingSystem(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		turn       int
		within     bool
	}{
		{"begins", `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"read it"}]},{"role":"system","content":"note"}]}`, 1, false},
		{"tool round", `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"read it"}]},{"role":"system","content":"note"},` +
			`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]},` +
			`{"role":"system","content":[{"type":"text","text":"reminder"}]}]}`, 1, true},
		{"next turn", `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"read it"}]},` +
			`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]},` +
			`{"role":"assistant","content":"done"},{"role":"user","content":"more"},{"role":"system","content":"note"}]}`, 2, false},
	} {
		turn, within := turnOf(provider.Anthropic, []byte(tc.body))
		if turn != tc.turn || within != tc.within {
			t.Errorf("%s: turn %d within %v", tc.name, turn, within)
		}
	}
}
