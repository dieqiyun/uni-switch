package gateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #292: a uni-switch model's summary goes to a relay in front of the ChatGPT
// backend as Codex asks for its own — streamed, with its tool_choice and
// include, under its thread's prompt_cache_key — or the relay answers
// "invalid codex request". The summary comes back from the stream.
func TestCodexCompactsThroughCodexRelay(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"resp_r1","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SUMM"},{"type":"output_text","text":"ARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_r1","status":"completed","output":[],"usage":{"input_tokens":9,"output_tokens":2,"total_tokens":11}}}`)}
	f.refuse = func(body []byte) (int, string) {
		var q map[string]any
		json.Unmarshal(body, &q)
		inc, _ := q["include"].([]any)
		if q["stream"] != true || q["tool_choice"] != "auto" || q["parallel_tool_calls"] == nil ||
			q["prompt_cache_key"] != "thread-1" || len(inc) != 1 || inc[0] != "reasoning.encrypted_content" {
			return 400, `{"error":{"code":"invalid_responses_request","message":"invalid codex request (request id: 1)","type":"new_api_error"}}`
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("third-party compaction reached OpenAI")
		w.WriteHeader(400)
	})
	code, body := codexPost(t, `{"model":"fake/m1","instructions":"You are Codex","stream":true,"store":false,
	  "include":["reasoning.encrypted_content"],"prompt_cache_key":"thread-1","tool_choice":"auto","parallel_tool_calls":true,
	  "reasoning":{"effort":"medium"},"tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
	  "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the bug"}]},{"type":"compaction_trigger"}]}`)
	if code != 200 {
		t.Fatalf("%d %s\nrelay got %s", code, body, f.got)
	}
	// no tool offered, written as Codex writes its own summary request:
	// "tools": [] (#1511)
	if !strings.Contains(string(f.got), `"tools":[]`) || strings.Contains(string(f.got), "shell") ||
		!strings.Contains(string(f.got), "CONTEXT CHECKPOINT COMPACTION") {
		t.Errorf("relay got %s", f.got)
	}
	var item, done map[string]any
	for _, e := range events(body) {
		switch e["type"] {
		case "response.output_item.done":
			item = e["item"].(map[string]any)
		case "response.completed":
			done = e["response"].(map[string]any)
		}
	}
	enc, _ := item["encrypted_content"].(string)
	b, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, uniSwitchCompaction))
	if item["type"] != "compaction" || string(b) != "SUMMARY" || done == nil || done["id"] != "resp_r1" || done["usage"] == nil {
		t.Errorf("events: %s", body)
	}

	// a stream that fails is the compaction failing, not an empty summary
	f.refuse = nil
	f.reply = sse(`data: {"type":"response.failed","response":{"id":"resp_r2","status":"failed","error":{"code":"server_error","message":"upstream fell over"}}}`)
	code, body = codexPost(t, `{"model":"fake/m1","stream":true,"input":[{"type":"message","role":"user","content":"hi"},{"type":"compaction_trigger"}]}`)
	if code != 502 || !strings.Contains(body, "upstream fell over") {
		t.Errorf("failed stream: %d %s", code, body)
	}
}
