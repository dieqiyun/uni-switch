package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexTitleTwoFields is #1466's title request: Codex's schema with a
// description beside the title, on /v1 with x-openai-subagent.
func codexTitleTwoFields(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model":"fake/m1","stream":true,"max_output_tokens":256,"reasoning":{"effort":"low"},"input":[{"role":"user","content":[{"type":"input_text","text":"Generate a concise title and description for: Research methods for selecting the appearance of a virtual clothing presenter. Return the required JSON object."}]}],` +
		`"text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":true,"schema":{"type":"object","properties":{"title":{"type":"string","maxLength":36},"description":{"type":"string","minLength":1}},"required":["title","description"],"additionalProperties":false}}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-openai-subagent", "thread_title")
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// terminals is a Responses stream's terminal events.
func terminals(body string) []map[string]any {
	var out []map[string]any
	for _, ev := range events(body) {
		switch ev["type"] {
		case "response.completed", "response.incomplete", "response.failed":
			out = append(out, ev)
		}
	}
	return out
}

// A title the vendor cut short before any text (max_output_tokens, all of
// it spent reasoning) goes back to Codex as the response.incomplete it was,
// its reason and usage kept, the one terminal event in the stream — not as
// a completed reply with nothing in it (#1466). A title that came whole
// under the same schema still completes with it.
func TestCodexTitleIncompleteStaysIncomplete(t *testing.T) {
	cut := sse(
		`data: {"type":"response.created","response":{"id":"resp_cut","object":"response","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		`data: {"type":"response.incomplete","response":{"id":"resp_cut","object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning","id":"rs_1","summary":[]}],"usage":{"input_tokens":60,"output_tokens":256,"output_tokens_details":{"reasoning_tokens":256},"total_tokens":316}}}`)
	whole := sse(
		`data: {"type":"response.created","response":{"id":"resp_ok","object":"response","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"title\":\"Virtual presenter looks\",\"description\":\"Ways to choose a virtual clothing presenter's appearance\"}"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_ok","object":"response","status":"completed","output":[],"usage":{"input_tokens":60,"output_tokens":40,"total_tokens":100}}}`)
	f := &fake{t: t, ctype: "text/event-stream"}
	setup(t, provider.Responses, f)
	s := New()

	f.reply = cut
	rec := codexTitleTwoFields(t, s)
	ends := terminals(rec.Body.String())
	if rec.Code != 200 || len(ends) != 1 || ends[0]["type"] != "response.incomplete" {
		t.Fatalf("cut short: %d, terminal events %v: %s", rec.Code, ends, rec.Body.String())
	}
	resp, _ := ends[0]["response"].(map[string]any)
	details, _ := resp["incomplete_details"].(map[string]any)
	used, _ := resp["usage"].(map[string]any)
	out, _ := resp["output"].([]any)
	if resp["status"] != "incomplete" || details["reason"] != "max_output_tokens" || used["output_tokens"] != float64(256) || len(out) != 0 {
		t.Errorf("cut short: %v", resp)
	}

	f.reply = whole
	rec = codexTitleTwoFields(t, s)
	ends = terminals(rec.Body.String())
	if text, _ := outputText(t, rec.Body.String()); rec.Code != 200 || len(ends) != 1 ||
		text != `{"description":"Ways to choose a virtual clothing presenter's appearance","title":"Virtual presenter looks"}` {
		t.Fatalf("whole: %d %q", rec.Code, rec.Body.String())
	}
	if resp, _ := ends[0]["response"].(map[string]any); resp["status"] != "completed" || resp["incomplete_details"] != nil {
		t.Errorf("whole: %v", resp)
	}
}

// A Chat model's title cut short (finish_reason "length", no text) goes
// back incomplete too, for max_output_tokens.
func TestCodexTitleIncompleteFromChat(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":"Let me think"}}]}`,
		`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":60,"completion_tokens":256}}`,
		`data: [DONE]`)}
	setup(t, provider.Chat, f)
	rec := codexTitleTwoFields(t, New())
	ends := terminals(rec.Body.String())
	if rec.Code != 200 || len(ends) != 1 || ends[0]["type"] != "response.incomplete" {
		t.Fatalf("%d, terminal events %v: %s", rec.Code, ends, rec.Body.String())
	}
	resp, _ := ends[0]["response"].(map[string]any)
	if details, _ := resp["incomplete_details"].(map[string]any); details["reason"] != "max_output_tokens" || resp["usage"] == nil {
		t.Errorf("%v", resp)
	}
}
