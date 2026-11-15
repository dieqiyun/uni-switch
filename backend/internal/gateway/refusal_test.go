package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A reply the vendor's safety filter ended before anything was said in it
// (#248): Anthropic's stop_reason "refusal" after an empty text block, as
// Claude answered Codex through a group, 2 tokens out and the whole prompt
// read from the cache.
var anthropicRefusal = sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"m","content":[],"usage":{"input_tokens":3,"cache_read_input_tokens":237000,"cache_creation_input_tokens":47000}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`event: ping`+"\n"+`data: {"type":"ping"}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null},"usage":{"output_tokens":2}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

var anthropicAnswer = sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m2","role":"assistant","model":"m","content":[],"usage":{"input_tokens":3}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from b"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

// Chat Completions' content_filter, with nothing said before it.
var chatFiltered = sse(`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`,
	`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":0}}`,
	`data: [DONE]`)

// Responses' incomplete for content_filter, with an empty message begun.
var responsesFiltered = sse(`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
	`event: response.output_item.added`+"\n"+`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
	`event: response.content_part.added`+"\n"+`data: {"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
	`event: response.incomplete`+"\n"+`data: {"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[],"usage":{"input_tokens":3,"output_tokens":0,"total_tokens":3}}}`)

func responsesOn(t *testing.T, id string, s *scripted) {
	t.Helper()
	up := httptest.NewServer(s)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Models: []string{"m"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
}

func sendTo(s *Server, path, body string) (int, string) {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

const codexAsk = `{"model":"group/g","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`

// Chat Completions' content_filter after a canned refusal line as the
// whole content (#5075): CodeArts's word-list filter answers a fixed
// politeness line and has its finish code say the turn was filtered —
// 10/10 on the same body, the line byte-identical, while another member
// of the group answered 620 tokens. A group kept the member that answered
// with the line, its mates never asked; a client that reads finish
// reasons ended the whole turn on it.
var chatCannedRefusal = sse(`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"As an AI language model I have not learned to answer this, please ask me something else"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`,
	`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":44,"completion_tokens":47}}`,
	`data: [DONE]`)

var chatStreamsAnswer = sse(`data: {"id":"c2","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
	`data: {"id":"c2","choices":[{"index":0,"delta":{"content":"a pool is a container"}}]}`,
	`data: {"id":"c2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	`data: {"id":"c2","choices":[],"usage":{"prompt_tokens":44,"completion_tokens":5}}`,
	`data: [DONE]`)

func TestStreamEventRefusals(t *testing.T) {
	for _, x := range []struct {
		ev   string
		kind int
	}{
		{`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, eventLead},
		{`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t","name":"x","input":{}}}`, eventContent},
		{`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`, eventThinking},
		{`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"s"}}`, eventLead},
		{`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hm"}}`, eventThinking},
		{`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`, eventContent},
		{`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"refusal"}}`, eventRefusal},
		{`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, eventContent},
		{`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`, eventRefusal},
		{`data: {"id":"c","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"content_filter"}]}`, eventContent},
		{`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, eventContent},
		{`data: {"type":"response.output_item.added","item":{"type":"reasoning","summary":[]}}`, eventThinking},
		{`data: {"type":"response.reasoning_summary_text.delta","delta":"**Plan**"}`, eventThinking},
		{`data: {"id":"c","choices":[{"index":0,"delta":{"reasoning_content":"hm"}}]}`, eventThinking},
		{`data: {"id":"c","choices":[{"index":0,"delta":{"reasoning_content":"hm"},"finish_reason":"content_filter"}]}`, eventRefusal},
		{`data: {"candidates":[{"content":{"parts":[{"text":"hm","thought":true}]}}]}`, eventThinking},
		{`data: {"type":"response.output_item.added","item":{"type":"function_call","name":"x"}}`, eventContent},
		{`data: {"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"hi"}]}}`, eventContent},
		{`data: {"type":"response.output_text.delta","delta":""}`, eventLead},
		{`data: {"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`, eventThinking},
		{`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"content_filter"}}}`, eventRefusal},
		{`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, eventContent},
		{`data: {"type":"response.completed","response":{"status":"completed"}}`, eventContent},
		{`data: {"type":"error","error":{"code":"content_filter","message":"filtered"}}`, eventRefusalError},
		{`data: {"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`, eventRefusal},
		{`data: {"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"SAFETY"}]}`, eventContent},
		{`data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`, eventContent},
		{`data: {"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`, eventRefusal},
	} {
		if k, _, _ := streamEvent([]byte(x.ev)); k != x.kind {
			t.Errorf("streamEvent(%s) = %d, want %d", x.ev, k, x.kind)
		}
	}
}
