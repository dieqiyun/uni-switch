package gateway

import (
	"strings"
	"testing"
)

// jinccc-ctrl on #4659: with tools declared and the model answering in
// text alone, WorkBuddy's stream ends on a frame whose function_call is
// an empty object where OpenAI's contract has null. A client that checks
// every frame's shape reads the empty object for a legacy call and
// refuses the whole turn ("legacy function_call is unsupported"), so a
// reply of plain text is a turn lost whenever the client offered tools.
// An empty function_call — a name and arguments both empty — says
// nothing, so it is left out; a null goes as it came.
func TestStreamedEmptyFunctionCallLeftOut(t *testing.T) {
	fresh(t)
	wire := sse(
		`data: {"choices":[{"delta":{"content":"","extra_fields":null,"function_call":null,"refusal":"","role":"assistant","tool_calls":[]},"finish_reason":"","index":0,"logprobs":null}],"created":1793498714,"id":"bda44076918cd5aad55494cabc17cf51","model":"deepseek-v4.1-flash","object":"chat.completion.chunk","usage":null}`,
		`data: {"choices":[{"delta":{"content":"你好！","extra_fields":null,"function_call":null,"refusal":"","role":"assistant","tool_calls":[]},"finish_reason":"","index":0,"logprobs":null}],"created":1793498714,"id":"bda44076918cd5aad55494cabc17cf51","model":"deepseek-v4.1-flash","object":"chat.completion.chunk","usage":null}`,
		`data: {"choices":[{"delta":{"content":"","extra_fields":null,"function_call":{"name":"","arguments":""},"refusal":"","role":"assistant"},"finish_reason":"stop","index":0,"logprobs":null}],"created":0,"id":"bda44076918cd5aad55494cabc17cf51","model":"deepseek-v4.1-flash","object":"chat.completion.chunk","usage":null}`,
		`data: [DONE]`)
	a := &scripted{replies: []reply{{200, "text/event-stream", wire}}}
	serveOn(t, "a", "ka", []string{"m"}, a)
	s := New()
	ask := `{"model":"m","stream":true,"messages":[{"role":"user","content":"说声你好，别调工具"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"查天气","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],"max_tokens":100}`
	code, body := postAs(t, s, "", ask)
	if code != 200 || !strings.Contains(body, "你好！") {
		t.Fatalf("%d %s", code, body)
	}
	if strings.Contains(body, `"function_call":{"name":"","arguments":""}`) {
		t.Fatalf("an empty function_call object went out where the contract has null (#4659): %s", body)
	}
	if !strings.Contains(body, `"function_call":null`) {
		t.Fatalf("a function_call null did not go as it came: %s", body)
	}
}

// A legacy function_call that names a function is a call a client may
// still be reading, its arguments a later fragment's to finish: both go
// as they came, and so does the finish that names one.
func TestStreamedNamedFunctionCallKept(t *testing.T) {
	fresh(t)
	wire := sse(
		`data: {"choices":[{"delta":{"function_call":{"name":"get_weather","arguments":""}},"finish_reason":null,"index":0}]}`,
		`data: {"choices":[{"delta":{"function_call":{"name":"","arguments":"{\"city\":\"北京\"}"}},"finish_reason":null,"index":0}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"function_call","index":0}]}`,
		`data: [DONE]`)
	a := &scripted{replies: []reply{{200, "text/event-stream", wire}}}
	serveOn(t, "a", "ka", []string{"m"}, a)
	s := New()
	ask := `{"model":"m","stream":true,"messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
	code, body := postAs(t, s, "", ask)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if !strings.Contains(body, `"function_call":{"name":"get_weather","arguments":""}`) ||
		!strings.Contains(body, `"function_call":{"name":"","arguments":"{\"city\":\"北京\"}"}`) {
		t.Fatalf("a legacy call naming a function did not go as it came: %s", body)
	}
}
