package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// piDeferred is pi's request (openai-completions) after its tool_search
// loaded a tool: the history calls mcp__github__list_issues, and tools
// holds only read and tool_search (Gearclickist on Discord).
const piDeferred = `{"model":"fake/m1","stream":false,"messages":[
{"role":"system","content":"You are pi."},
{"role":"user","content":"list my open issues"},
{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"github issues\"}"}}]},
{"role":"tool","tool_call_id":"call_1","content":"Loaded 1 tool. They are available from your next call:\n- mcp__github__list_issues: List issues"},
{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"mcp__github__list_issues","arguments":"{\"state\":\"open\"}"}}]},
{"role":"tool","tool_call_id":"call_2","content":"#1 bug"},
{"role":"user","content":"thanks, and the closed ones?"}],
"tools":[{"type":"function","function":{"name":"read","description":"Read a file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}},
{"type":"function","function":{"name":"tool_search","description":"Search tools","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}}]}`

// piToolless is pi's request with no tools left to offer: openai-completions
// sends "tools": [] beside the history of calls.
var piToolless = strings.Replace(piDeferred, piDeferred[strings.Index(piDeferred, `"tools":[`):], `"tools":[]}`, 1)

const (
	histChatOK      = `{"id":"c","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	histResponsesOK = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"model\":\"m1\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
	histAnthropicOK = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m1\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
)

// declaredIn is the tool names body's tools declare, on proto.
func declaredIn(proto provider.Protocol, body []byte) map[string]bool {
	out := map[string]bool{}
	for _, t := range gjson.GetBytes(body, "tools").Array() {
		n := t.Get("name").String()
		if proto == provider.Chat {
			n = t.Get("function.name").String()
		}
		out[n] = true
	}
	return out
}

func postChatTo(h http.Handler, body string) (int, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// A Chat or Responses upstream that checks each history call's name
// against tools turns pi's request away naming the tool: it is asked once
// more with a stub declared for it, and the next request carries the
// stub from the start.
func TestUndeclaredHistoryToolRefusedThenStubbed(t *testing.T) {
	for _, tc := range []struct {
		proto   provider.Protocol
		ok      string
		refusal string
	}{
		{provider.Chat, histChatOK, `{"error":{"message":"Tool 'mcp__github__list_issues' not found in tools","type":"invalid_request_error","code":"invalid_tool"}}`},
		{provider.Responses, histResponsesOK, `{"error":{"message":"Invalid input[3]: function 'mcp__github__list_issues' is not defined in tools.","type":"invalid_request_error","param":"input"}}`},
		{provider.Anthropic, histAnthropicOK, `{"type":"error","error":{"type":"invalid_request_error","message":"Improperly formed request: tool mcp__github__list_issues not found in toolConfig"}}`},
	} {
		t.Run(string(tc.proto), func(t *testing.T) {
			f := &fake{t: t, ctype: ctypeOf(tc.proto), reply: tc.ok}
			var asked [][]byte
			f.refuse = func(b []byte) (int, string) {
				asked = append(asked, b)
				if !declaredIn(tc.proto, b)["mcp__github__list_issues"] {
					return 400, tc.refusal
				}
				return 0, ""
			}
			setup(t, tc.proto, f)
			h := New().Handler()
			code, out := postChatTo(h, piDeferred)
			if code != 200 || len(asked) != 2 {
				t.Fatalf("code %d after %d asks, want 200 after 2: %s", code, len(asked), out)
			}
			last := asked[1]
			d := declaredIn(tc.proto, last)
			if !d["read"] || !d["tool_search"] || len(d) != 3 {
				t.Fatalf("declared %v, want read, tool_search and the stub: %s", d, last)
			}
			if c := gjson.GetBytes(last, "tool_choice"); c.Exists() {
				t.Fatalf("tool_choice %s set where the client offered tools", c.Raw)
			}
			if strings.Contains(strings.ToLower(string(last)), "uni-switch") {
				t.Fatalf("the stub names uni-switch: %s", last)
			}
			asked = nil
			if code, out := postChatTo(h, piDeferred); code != 200 || len(asked) != 1 {
				t.Fatalf("next request: code %d after %d asks, want 200 after 1 (remembered): %s", code, len(asked), out)
			}
		})
	}
}

// Anthropic turns away tool_use blocks with no tools ("Requests which
// include `tool_use` or `tool_result` blocks must define tools"): pi's
// "tools": [] goes with a stub for each tool the history called, which
// none may be called.
func TestToollessHistoryToAnthropicDeclaresStubs(t *testing.T) {
	f := &fake{t: t, reply: histAnthropicOK}
	var asked [][]byte
	f.refuse = func(b []byte) (int, string) {
		asked = append(asked, b)
		if strings.Contains(string(b), `"tool_use"`) && len(gjson.GetBytes(b, "tools").Array()) == 0 {
			return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"Requests which include ` + "`tool_use`" + ` or ` + "`tool_result`" + ` blocks must define tools."}}`
		}
		return 0, ""
	}
	setup(t, provider.Anthropic, f)
	code, out := post(t, "/v1/chat/completions", piToolless)
	if code != 200 || len(asked) != 1 {
		t.Fatalf("code %d after %d asks, want 200 at once: %s", code, len(asked), out)
	}
	d := declaredIn(provider.Anthropic, asked[0])
	if !d["tool_search"] || !d["mcp__github__list_issues"] || len(d) != 2 {
		t.Fatalf("declared %v, want the two tools the history called: %s", d, asked[0])
	}
	if c := gjson.GetBytes(asked[0], "tool_choice.type").String(); c != "none" {
		t.Fatalf("tool_choice %q, want none: the client offered no tools", c)
	}
}

// pi's "tools": [] to a Chat upstream that wants tools beside the calls
// (a relay in front of Anthropic passing its refusal on) is asked once
// more with the stubs, and tool_choice none.
func TestToollessHistoryRefusedOnChat(t *testing.T) {
	f := &fake{t: t, ctype: "application/json", reply: histChatOK}
	var asked [][]byte
	f.refuse = func(b []byte) (int, string) {
		asked = append(asked, b)
		if len(gjson.GetBytes(b, "tools").Array()) == 0 {
			return 400, `{"error":{"message":"litellm.BadRequestError: AnthropicException - {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"Requests which include ` + "`tool_use`" + ` or ` + "`tool_result`" + ` blocks must define tools.\"}}","type":null,"param":null,"code":"400"}}`
		}
		return 0, ""
	}
	setup(t, provider.Chat, f)
	code, out := post(t, "/v1/chat/completions", piToolless)
	if code != 200 || len(asked) != 2 {
		t.Fatalf("code %d after %d asks, want 200 after 2: %s", code, len(asked), out)
	}
	if d := declaredIn(provider.Chat, asked[1]); !d["tool_search"] || !d["mcp__github__list_issues"] {
		t.Fatalf("declared %v: %s", d, asked[1])
	}
	if c := gjson.GetBytes(asked[1], "tool_choice").String(); c != "none" {
		t.Fatalf("tool_choice %q, want none", c)
	}
}

// An upstream that takes the history as it is (OpenAI, Anthropic with
// tools, Gemini) is sent it as it was: no stub, and one ask.
func TestUndeclaredHistoryToolTakenAsSent(t *testing.T) {
	for _, tc := range []struct {
		proto provider.Protocol
		ok    string
	}{{provider.Chat, histChatOK}, {provider.Responses, histResponsesOK}, {provider.Anthropic, histAnthropicOK}} {
		t.Run(string(tc.proto), func(t *testing.T) {
			f := &fake{t: t, ctype: ctypeOf(tc.proto), reply: tc.ok}
			setup(t, tc.proto, f)
			if code, out := post(t, "/v1/chat/completions", piDeferred); code != 200 || f.calls != 1 {
				t.Fatalf("code %d after %d asks: %s", code, f.calls, out)
			}
			if d := declaredIn(tc.proto, f.got); d["mcp__github__list_issues"] || len(d) != 2 {
				t.Fatalf("declared %v, want read and tool_search alone: %s", d, f.got)
			}
		})
	}
}

// A refusal over something else, or naming a tool the request declares,
// is not this one.
func TestRefusesHistoryToolsMatch(t *testing.T) {
	names := []string{"mcp__github__list_issues"}
	for _, b := range []string{
		`{"error":{"message":"Tool 'mcp__github__list_issues' not found in tools"}}`,
		`{"error":{"message":"unknown tool: mcp__github__list_issues"}}`,
		`{"error":{"message":"function \"mcp__github__list_issues\" does not exist"}}`,
	} {
		if !refusesHistoryTools([]byte(b), names, false) {
			t.Errorf("not matched: %s", b)
		}
	}
	for _, b := range []string{
		`{"error":{"message":"Tool 'read' not found in tools"}}`,
		`{"error":{"message":"Invalid value for 'temperature'"}}`,
		`{"error":{"message":"mcp__github__list_issues_extra not found"}}`,
		`{"error":{"message":"Requests which include tool_use or tool_result blocks must define tools."}}`,
	} {
		if refusesHistoryTools([]byte(b), names, false) {
			t.Errorf("matched: %s", b)
		}
	}
	if !refusesHistoryTools([]byte(`Requests which include tool_use or tool_result blocks must define tools.`), names, true) {
		t.Error("must define tools not matched with no tools")
	}
}

// ctypeOf is the fake's reply type: a Chat client's request is relayed to
// a Chat upstream as sent (not streamed), and translated, streamed, to
// the others.
func ctypeOf(proto provider.Protocol) string {
	if proto == provider.Chat {
		return "application/json"
	}
	return ""
}
