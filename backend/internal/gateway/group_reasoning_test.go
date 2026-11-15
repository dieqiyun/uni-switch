package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// quietUp is a Chat vendor that answers as its key, and keeps what it was
// sent; fail makes it answer 500, so the group falls to the next member.
// It speaks Responses too, for a request sent that way, and streams a
// Chat request that asks to be.
type quietUp struct {
	key  string
	mu   sync.Mutex
	last string
	fail int
}

func (u *quietUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.last = string(b)
	fail := u.fail
	u.mu.Unlock()
	if fail != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(fail)
		io.WriteString(w, `{"error":{"message":"down"}}`)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/responses") {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r1","model":"m","usage":{"input_tokens":0,"output_tokens":0}}}`,
			`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"from `+u.key+`"}`,
			`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r1","model":"m","status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`,
		))
		return
	}
	if strings.Contains(string(b), `"stream":true`) {
		// a translated request (a Gemini client's) is streamed
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from `+u.key+`"}}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`data: [DONE]`,
		))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"from `+u.key+`"},"finish_reason":"stop"}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
}

func (u *quietUp) sent() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	var v map[string]any
	json.Unmarshal([]byte(u.last), &v)
	return v
}

// The request the quiet member is sent keeps every other field: only what
// asks the model to think goes, in the agent's own protocol. Anthropic's
// thinking turned off is no ask and stays; output_config keeps its other
// keys. Gemini's thinkingConfig goes when it asks to think; a budget of 0
// is no ask and stays.
func TestWithoutReasoningAsk(t *testing.T) {
	for _, c := range []struct {
		name  string
		proto provider.Protocol
		in    string
		want  string
	}{
		{"chat", provider.Chat, `{"model":"m","reasoning_effort":"high","temperature":0.2}`, `{"model":"m","temperature":0.2}`},
		{"chat reasoning", provider.Chat, `{"model":"m","reasoning":{"effort":"high","summary":"auto"}}`, `{"model":"m"}`},
		{"chat nothing", provider.Chat, `{"model":"m","messages":[]}`, `{"model":"m","messages":[]}`},
		{"responses", provider.Responses, `{"model":"m","reasoning":{"effort":"high","context":"all_turns"}}`, `{"model":"m"}`},
		{"anthropic on", provider.Anthropic, `{"model":"m","thinking":{"type":"enabled","budget_tokens":4096}}`, `{"model":"m"}`},
		{"anthropic adaptive", provider.Anthropic, `{"model":"m","thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`, `{"model":"m"}`},
		{"anthropic off", provider.Anthropic, `{"model":"m","thinking":{"type":"disabled"}}`, `{"model":"m","thinking":{"type":"disabled"}}`},
		{"gemini level", provider.Gemini, `{"model":"m","generationConfig":{"temperature":0.2,"thinkingConfig":{"thinkingLevel":"high","includeThoughts":true}}}`, `{"generationConfig":{"temperature":0.2},"model":"m"}`},
		{"gemini budget", provider.Gemini, `{"model":"m","generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`, `{"model":"m"}`},
		{"gemini off", provider.Gemini, `{"generationConfig":{"thinkingConfig":{"thinkingBudget":0}},"model":"m"}`, `{"generationConfig":{"thinkingConfig":{"thinkingBudget":0}},"model":"m"}`},
		{"anthropic output", provider.Anthropic, `{"model":"m","output_config":{"effort":"high","format":{"type":"json"}}}`, `{"model":"m","output_config":{"format":{"type":"json"}}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := string(withoutReasoningAsk(c.proto, []byte(c.in)))
			if got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}
