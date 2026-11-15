package gateway

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// vendorDeveloper400 is what a chat-role upstream answers a request
// carrying the developer role with (DeepSeek among them, #2955).
const vendorDeveloper400 = `{"error":{"code":"invalid_parameter_error","message":"developer is not one of ['system', 'assistant', 'user', 'tool', 'function']","param":null,"type":"invalid_request_error"}}`

// #2955: Codex on a provider whose Responses endpoint validates chat
// roles — a relay of a chat-only vendor's among them, DeepSeek's preset
// base alike: every compaction failed there with 400 "developer is not
// one of ['system', 'assistant', 'user', 'tool', 'function']" while the
// turns around it passed, and Codex retried each five times. The summary
// request is relayed as it came, and the compaction carries a developer
// message its turns don't. The summary ask uni-switch makes itself goes to a
// provider that isn't OpenAI's with its developer messages as the system
// ones they are, where they stand: what Zed's cloud is given (ZedBody),
// and what a Chat Completions upstream is given (developerAsSystem).
func TestCodexCompactDeveloperAsSystemOnAVendorRelay(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"resp_v1","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_v1","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		if strings.Contains(string(body), `"role":"developer"`) {
			return http.StatusBadRequest, vendorDeveloper400
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, `{"model":"fake/m1","stream":true,"store":false,
	  "input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are Codex, a coding agent."}]},
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},
	  {"type":"message","role":"assistant","content":[{"type":"output_text","text":"I fixed main.go"}]},
	  {"type":"compaction_trigger"}]}`)
	if code != 200 || !strings.Contains(body, `"compaction"`) {
		t.Fatalf("%d %s\nupstream got %s", code, body, f.got)
	}
	if got := compactedText(t, body); got != "SUMMARY" {
		t.Errorf("summary %q", got)
	}
	if strings.Contains(string(f.got), `"role":"developer"`) {
		t.Errorf("developer went over the wire: %s", f.got)
	}
	// the message itself stays, where it stood, as a system one
	if !strings.Contains(string(f.got), `"role":"system"`) || !strings.Contains(string(f.got), "You are Codex, a coding agent.") {
		t.Errorf("system message lost: %s", f.got)
	}
	for _, want := range []string{"fix the login bug", "I fixed main.go", "CONTEXT CHECKPOINT COMPACTION"} {
		if !strings.Contains(string(f.got), want) {
			t.Errorf("summary request lacks %q: %s", want, f.got)
		}
	}
}

// A turn on the same provider goes as it came, developer message and all:
// only uni-switch's own summary ask is fitted, never Codex's own turn, whose
// shape a relay may be checking (#350, #1758).
func TestVendorRelayTurnKeepsItsDeveloperMessage(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.completed","response":{"id":"resp_v2","status":"completed","output":[]}}`)}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, `{"model":"fake/m1","stream":true,"store":false,
	  "input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are Codex, a coding agent."}]},
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if !strings.Contains(string(f.got), "hello") || !strings.Contains(string(f.got), `"role":"developer"`) {
		t.Errorf("turn's developer message changed: %s", f.got)
	}
}
