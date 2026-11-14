package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// AricRedemption on #1475: a long Qoder turn ended with an upstream tool
// call whose function.name was no string, and the client that checks the
// shape of every frame (the AI SDK) refused the whole reply — "Expected
// 'function.name' to be a string" — the turn not retryable and the call
// already in the history, so the session was dead; uni-switch's own log said
// status=200, done=true. A call no client can read runs nowhere, so it is
// no call: it is dropped, its later fragments with it, and a finish of
// tool_calls with no call left ends stop — what textcalls' parser does
// with a written block that names no offered tool. A name the client
// reads as a string, empty included, goes as it came.

// The first fragment of a streamed call naming no function in a string,
// its argument fragments dropped with it, a finish of tool_calls ended
// stop, the text before the call kept.
func TestNamelessCallStreamDropped(t *testing.T) {
	first := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_up","type":"function","function":{"name":null,"arguments":""}}]},"finish_reason":null}]}`
	args := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":\"ls -la\"}"}}]},"finish_reason":null}]}`
	ended := strings.Replace(stopChunk, `"stop"`, `"tool_calls"`, 1)
	text, calls, finish, raw := relayedChat(t,
		`{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],`+execChatTool+`}`,
		chunkOf("Running it."), first, args, ended)
	if text != "Running it." || len(calls) != 0 || finish != "stop" {
		t.Fatalf("text %q, calls %v, finish %q\n%s", text, calls, finish, raw)
	}
	if strings.Contains(raw, `"tool_calls":[`) {
		t.Fatalf("a call naming no function reached the client:\n%s", raw)
	}
}

// The same with the name field absent, as the merged deltas leave it for
// a client to read.
func TestNameAbsentStreamDropped(t *testing.T) {
	first := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_up","type":"function","function":{"arguments":""}}]},"finish_reason":null}]}`
	args := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":\"ls\"}"}}]},"finish_reason":null}]}`
	_, calls, finish, raw := relayedChat(t,
		`{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],`+execChatTool+`}`,
		first, args, stopChunk)
	if len(calls) != 0 || finish != "stop" {
		t.Fatalf("calls %v, finish %q\n%s", calls, finish, raw)
	}
	if strings.Contains(raw, `"tool_calls":[`) {
		t.Fatalf("a call naming no function reached the client:\n%s", raw)
	}
}

// The same dropped call the upstream gave no id for: the call is dropped,
// yet it comes back — the drop takes it out of the delta, but the
// fragments themselves stay in the local calls, and the id mend that
// follows (#2611) gives the first one an id and writes them back. The
// frame the client refuses leaves after all, an id on it.
func TestNamelessCallWithoutAnIDStaysDropped(t *testing.T) {
	first := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":null,"arguments":""}}]},"finish_reason":null}]}`
	args := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":\"ls -la\"}"}}]},"finish_reason":null}]}`
	ended := strings.Replace(stopChunk, `"stop"`, `"tool_calls"`, 1)
	_, calls, finish, raw := relayedChat(t,
		`{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],`+execChatTool+`}`,
		chunkOf("Running it."), first, args, ended)
	if len(calls) != 0 || finish != "stop" {
		t.Fatalf("calls %v, finish %q\n%s", calls, finish, raw)
	}
	if strings.Contains(raw, `"tool_calls":[`) {
		t.Fatalf("the call dropped for naming no function came back with an id:\n%s", raw)
	}
}

// A call with a name stands beside one without: the named one goes, the
// nameless one alone is dropped, and the finish stays tool_calls.
func TestNamelessCallBesideAGoodOne(t *testing.T) {
	mixed := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_up","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"ls\"}"}},{"index":1,"id":"call_bad","type":"function","function":{"name":null,"arguments":""}}]},"finish_reason":null}]}`
	_, calls, finish, raw := relayedChat(t,
		`{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],`+execChatTool+`}`,
		mixed, stopChunk)
	if len(calls) != 1 || calls["exec_command"] != `{"cmd":"ls"}` || finish != "tool_calls" {
		t.Fatalf("calls %v, finish %q\n%s", calls, finish, raw)
	}
	if strings.Contains(raw, "call_bad") {
		t.Fatalf("the nameless call reached the client:\n%s", raw)
	}
}

// A whole reply's call naming no function is dropped before it is
// relayed, and its finish of tool_calls ends stop; the text is kept.
func TestNamelessCallWholeDropped(t *testing.T) {
	up := `{"id":"c1","object":"chat.completion","created":1791370000,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"Let me.","tool_calls":[{"id":"call_up","type":"function","function":{"name":null,"arguments":"{\"cmd\":\"ls\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	text, calls, finish, raw := wholeChat(t,
		`{"model":"a/m","stream":false,"messages":[{"role":"user","content":"go"}],`+execChatTool+`}`,
		"application/json", up)
	if text == nil || *text != "Let me." || len(calls) != 0 || finish != "stop" {
		t.Fatalf("text %v, calls %v, finish %q\n%s", text, calls, finish, raw)
	}
	if strings.Contains(raw, `"tool_calls"`) {
		t.Fatalf("a call naming no function reached the client:\n%s", raw)
	}
}

// The whole reply a vendor gave after the agent was kept alive goes out
// as the stream the agent asked for (wholeAsStream): its call naming no
// function is dropped there too, and the finish with no call left ends
// stop.
func TestNamelessCallWholeAsStreamDropped(t *testing.T) {
	up := `{"id":"c1","object":"chat.completion","created":1791370000,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"Let me.","tool_calls":[{"id":"call_up","type":"function","function":{"name":null,"arguments":"{\"cmd\":\"ls\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	var sawToolCalls, saidNull bool
	var finish string
	for _, e := range chatWholeEvents([]byte(up)) {
		b, _ := json.Marshal(e.data)
		s := string(b)
		if strings.Contains(s, `"name":null`) || strings.Contains(s, `"name":`) && !strings.Contains(s, `"name":"`) {
			saidNull = true
		}
		if strings.Contains(s, `"tool_calls"`) {
			sawToolCalls = true
		}
		var ch struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(s), &ch) == nil && len(ch.Choices) > 0 && ch.Choices[0].FinishReason != "" {
			finish = ch.Choices[0].FinishReason
		}
	}
	if sawToolCalls || saidNull {
		t.Fatalf("the events carried a call naming no function (tool_calls %v, null name %v)", sawToolCalls, saidNull)
	}
	if finish != "stop" {
		t.Fatalf("finish %q, want stop", finish)
	}
}

// A whole reply given as a stream with a named call beside a nameless
// one: the named call goes, numbered 0 with no gap where the dropped one
// stood, and the finish stays tool_calls, since a call is left.
func TestNamelessCallBesideAGoodOneWholeAsStream(t *testing.T) {
	up := `{"id":"c1","object":"chat.completion","created":1791370000,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_bad","type":"function","function":{"name":null,"arguments":""}},{"id":"call_up","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"ls\"}"}}]},"finish_reason":"tool_calls"}]}`
	var finish string
	var got []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	for _, e := range chatWholeEvents([]byte(up)) {
		if _, done := e.data.(string); done {
			continue // [DONE]
		}
		b, _ := json.Marshal(e.data)
		var ch struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(b, &ch); err != nil {
			t.Fatalf("unreadable event %s: %v", b, err)
		}
		for _, c := range ch.Choices {
			for _, tc := range c.Delta.ToolCalls {
				got = append(got, struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				}(tc))
			}
			if c.FinishReason != nil {
				finish = *c.FinishReason
			}
		}
	}
	if len(got) != 1 || got[0].ID != "call_up" || got[0].Index != 0 || got[0].Function.Name != "exec_command" || finish != "tool_calls" {
		t.Fatalf("calls %+v, finish %q", got, finish)
	}
}

// A whole reply that ends tool_calls with no call in it at all keeps its
// finish as it came: only a reply whose calls were dropped for naming no
// function ends stop.
func TestToolCallsFinishWithNoCallsStaysWhole(t *testing.T) {
	up := `{"id":"c1","object":"chat.completion","created":1791370000,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"Done."},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	text, _, finish, raw := wholeChat(t,
		`{"model":"a/m","stream":false,"messages":[{"role":"user","content":"go"}],`+execChatTool+`}`,
		"application/json", up)
	if text == nil || *text != "Done." || finish != "tool_calls" {
		t.Fatalf("text %v, finish %q\n%s", text, finish, raw)
	}
}
