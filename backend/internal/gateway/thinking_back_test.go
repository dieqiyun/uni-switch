package gateway

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What api.deepseek.com answered on 2026-11-02, byte for byte, to a tool
// loop continued with a tool turn that has no thinking: on its Messages
// API and on its Chat API.
const (
	deepseekThinkingBackLive  = `{"error":{"message":"The ` + "`content[].thinking`" + ` in the thinking mode must be passed back to the API. (request_id: cae816b6-d668-4a55-935d-99d1b8ad3686)","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`
	deepseekReasoningBackLive = `{"error":{"message":"The ` + "`reasoning_content`" + ` in the thinking mode must be passed back to the API. (request_id: 6360375a-b044-4a13-b51e-548de94677ea)","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`
	// qisishiqi's error on Discord (#help, 2026-11-02), as uni-switch passed it on
	qisishiqiError = `{"code":null,"message":"My Claude: The ` + "`content[].thinking`" + ` in the thinking mode must be passed back to the API.","param":null,"type":"invalid_request_error"}`
)

// thinkingBackStream is the Messages stream the fakes answer with.
var thinkingBackStream = sse(
	`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m1","usage":{"input_tokens":7}}}`,
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Done."}}`,
	`data: {"type":"content_block_stop","index":0}`,
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	`data: {"type":"message_stop"}`)

// codexMidLoop is Codex continuing a tool loop its own model began: the
// reasoning it has for the call is OpenAI's, sealed, with no text.
const codexMidLoop = `{"model":"%s","stream":false,"reasoning":{"effort":"high"},
	"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}],
	"input":[
	{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},
	{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gAAAA-codex-sealed"},
	{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"ls\"}"},
	{"type":"function_call_output","call_id":"call_1","output":"main.go"}]}`

func TestThinkingBackRefusalRead(t *testing.T) {
	for _, s := range []string{deepseekThinkingBackLive, deepseekReasoningBackLive, qisishiqiError,
		`{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API."}}`} {
		if !thinkingBackRefusal.Match(wordsOf([]byte(s))) {
			t.Errorf("not read as asking for thinking back: %s", s)
		}
	}
	for _, s := range []string{
		"The `reasoning_text` in the thinking mode must be passed back", // DeepSeek's Responses, its own path
		"messages.1.content.0: Invalid signature in thinking block",
		"prompt is too long",
	} {
		if thinkingBackRefusal.MatchString(s) {
			t.Errorf("%q read as asking for thinking back", s)
		}
	}
}

// The sibling on Chat: DeepSeek's model behind a Chat provider whose host
// and model don't say so, given Codex's loop, asks for reasoning_content
// back; uni-switch asks once more with it, and from then on.
func TestReasoningBackLearnedBehindAnotherName(t *testing.T) {
	f := &fake{t: t, reply: chatStreamOK, refuse: func(b []byte) (int, string) {
		if code, _ := deepseekRefuseToolTurns(b); code == 400 {
			return 400, deepseekReasoningBackLive
		}
		return 0, ""
	}}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "relay", Models: []string{"relay-model"}})
	srv := New()
	req := strings.Replace(codexMidLoop, "%s", "relay/relay-model", 1)
	code, body := postTo(t, srv, "/v1/responses", req)
	if code != 200 || f.calls != 2 {
		t.Fatalf("%d after %d calls, want 200 on the second: %s", code, f.calls, body)
	}
	if rc := gjson.GetBytes(f.got, `messages.#(role=="assistant").reasoning_content`); !rc.Exists() || rc.String() != noReasoning {
		t.Errorf("the tool turn's reasoning_content = %s, want a space: %s", rc.Raw, f.got)
	}
	f.calls = 0
	if code, body = postTo(t, srv, "/v1/responses", req); code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls, want 200 on the first: %s", code, f.calls, body)
	}
}
