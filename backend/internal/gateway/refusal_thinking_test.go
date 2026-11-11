package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Claude refusing after it has only thought, as Opus did Claude Code
// through a group (#248): thinking streamed, then stop_reason "refusal"
// with no word of the reply said. 381–707 tokens out, 200 on the routing
// page, and Claude Code's "…'s safeguards stopped the response above".
var anthropicThoughtThenRefused = sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"m","content":[],"usage":{"input_tokens":2,"cache_read_input_tokens":32749}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The user wants a plan for"}}`,
	`event: ping`+"\n"+`data: {"type":"ping"}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" the next step."}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null},"usage":{"output_tokens":381}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

var anthropicThoughtThenAnswered = sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"m","content":[]}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hm"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"from a"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":1}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

const claudeCodeAsk = `{"model":"group/g","max_tokens":10,"stream":true,"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"hi"}]}`

// Reasoning keeps the stream held past the 15s a quiet one is: Claude
// thought for 16–25s before refusing (#248). Past holdThinking it goes
// through, as it did.
func TestThinkingHeldLonger(t *testing.T) {
	events := strings.SplitAfter(anthropicThoughtThenRefused, "\n\n")
	start := func(ago time.Duration) *holdWriter {
		h := newHoldWriter(httptest.NewRecorder(), true)
		h.Header().Set("Content-Type", "text/event-stream")
		h.WriteHeader(http.StatusOK)
		h.since = time.Now().Add(-ago)
		return h
	}
	// the reply begins at once; its thinking goes on past holdLongest
	h := start(0)
	h.Write([]byte(events[0] + events[1]))
	h.since = time.Now().Add(-holdLongest - 10*time.Second)
	for _, ev := range events[2:] {
		h.Write([]byte(ev))
	}
	h.settle()
	if h.passing || !h.refused || !h.failed() {
		t.Fatalf("thought 25s then refused: passing %v refused %v", h.passing, h.refused)
	}
	h = start(holdThinking + time.Second)
	h.Write([]byte(events[0] + events[1] + events[2]))
	if !h.passing {
		t.Fatal("reasoning held past holdThinking")
	}
}
