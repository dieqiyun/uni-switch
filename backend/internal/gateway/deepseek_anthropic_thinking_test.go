package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// deepseekRefuseAnthropic answers as api.deepseek.com/anthropic does
// (deepseek-flash and deepseek-v4-pro, checked 2026-10-11 for #1611): a
// redacted_thinking block anywhere is a body it can't read (422), and,
// thinking (no thinking field, enabled or adaptive), an assistant message
// with a tool_use and no thinking block turns the whole request away (400).
// Thinking disabled, a bare tool_use is taken.
func deepseekRefuseAnthropic(body []byte) (int, string) {
	var q struct {
		Thinking *struct {
			Type string `json:"type"`
		} `json:"thinking"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &q)
	thinking := q.Thinking == nil || q.Thinking.Type != "disabled"
	// afterTool: the message before is tool results alone, which a reply
	// with no thinking is refused after too (checked 2026-10-12, #1822)
	afterTool := false
	for i, m := range q.Messages {
		var blocks []struct {
			Type string `json:"type"`
		}
		json.Unmarshal(m.Content, &blocks)
		after := afterTool
		afterTool = m.Role == "user" && len(blocks) > 0
		thought, called := false, false
		for _, b := range blocks {
			afterTool = afterTool && b.Type == "tool_result"
			switch b.Type {
			case "redacted_thinking":
				return 422, `{"error":{"message":"Failed to deserialize the JSON body into the target type: messages[` + string(rune('0'+i)) + "].content: unknown variant `redacted_thinking`, expected one of `text`, `image`, `document`, `thinking`, `server_tool_use`, `tool_use`, `tool_result`" + `","type":"invalid_request_error"}}`
			case "thinking":
				thought = true
			case "tool_use":
				called = true
			}
		}
		if thinking && m.Role == "assistant" && (called || after) && !thought {
			return 400, `{"error":{"message":"The ` + "`content[].thinking`" + ` in the thinking mode must be passed back to the API. (request_id: e689e65a-f4f1-4efb-a4e0-44b98b5325d4)","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`
		}
	}
	return 0, ""
}

const anthropicWholeOK = `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// a Claude Code conversation a routing group served with Claude first:
// Claude's redacted thinking before one call, Claude's signed thinking
// before another, a second step with none, and a reply with none
const claudeTurnsForDeepSeek = `{"model":"%s","max_tokens":32000,"stream":false,"thinking":{"type":"adaptive"},
	"tools":[{"name":"Bash","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}],
	"messages":[
	{"role":"user","content":"fix the login bug"},
	{"role":"assistant","content":[{"type":"redacted_thinking","data":"EmwKAhgBEgy3va3pzix"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"main.go"}]},
	{"role":"assistant","content":[{"type":"thinking","thinking":"Read main.go.","signature":"EqQBCkgIBxABGAIiQL"},{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"cat main.go"}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"package main"}]},
	{"role":"assistant","content":[{"type":"text","text":"Checking the test."},{"type":"tool_use","id":"toolu_3","name":"Bash","input":{"command":"go test"}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_3","content":"ok"}]},
	{"role":"assistant","content":[{"type":"redacted_thinking","data":"EmwKAhgBEgy3va3pzix"},{"type":"thinking","thinking":"Fine.","signature":"EqQBCkgIBxAB"},{"type":"text","text":"It passes."}]},
	{"role":"user","content":"thanks, now commit"}]}`

func sentAnthropicMessages(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	var q struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return q.Messages
}

// The rule holds with thinking left out (DeepSeek's default is to think):
// Claude Code's body without thinking is told thinking is off, and a
// tool_use turn still gets its block.
func TestDeepSeekAnthropicNoThinkingField(t *testing.T) {
	f := &fake{t: t, ctype: "application/json", reply: anthropicWholeOK, refuse: deepseekRefuseAnthropic}
	up := setup(t, provider.Anthropic, f)
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DS", Key: "k", Models: []string{"deepseek-v4-pro"}, Anthropic: up.URL}); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(strings.Replace(claudeTurnsForDeepSeek, `"thinking":{"type":"adaptive"},`, "", 1), "%s", "ds/deepseek-v4-pro", 1)
	if code, b := postTo(t, New(), "/v1/messages", body); code != 200 {
		t.Fatalf("%d: %s", code, b)
	}
	if ms := sentAnthropicMessages(t, f.got); !strings.HasPrefix(string(ms[1]), `{"content":[{"type":"thinking","thinking":" ","signature":""}`) {
		t.Errorf("first tool turn sent as %s", ms[1])
	}
}

// openRouterRefuseAnthropic answers as OpenRouter's /api/v1/messages did
// for a DeepSeek model in #2883: its request schema wants a thinking
// block's signature as a string, so one with no signature is "Invalid
// Anthropic Messages API request" (400), its metadata naming the block's
// missing signature. The fields are the ones the report quotes. Behind
// it, the model still wants a thinking block on each tool turn
// (deepseekRefuseAnthropic).
func openRouterRefuseAnthropic(body []byte) (int, string) {
	if code, msg := deepseekRefuseAnthropic(body); code != 0 {
		return code, msg
	}
	var q struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &q)
	for i, m := range q.Messages {
		var blocks []map[string]json.RawMessage
		json.Unmarshal(m.Content, &blocks)
		for j, b := range blocks {
			var kind, sig string
			json.Unmarshal(b["type"], &kind)
			if kind != "thinking" {
				continue
			}
			if raw, ok := b["signature"]; ok && json.Unmarshal(raw, &sig) == nil {
				continue
			}
			return 400, fmt.Sprintf(`{"error":{"message":"Invalid Anthropic Messages API request","code":400,"metadata":{"raw":[{"path":["messages",%d,"content"],"errors":[[],[{"path":[%d,"signature"],"expected":"string","code":"invalid_type","message":"Invalid input: expected string, received undefined"}]]}]}}}`, i, j)
		}
	}
	return 0, ""
}

// A DeepSeek model on OpenRouter's Anthropic endpoint (#2883): the
// thinking block a tool turn is given (a space, or the client's unsigned
// reasoning) goes with an empty signature, which OpenRouter's schema
// takes where it turns a missing one away. Both ways in are held to it:
// Claude Desktop's Messages request relayed as it came, and a Chat
// client's translated.
func TestOpenRouterDeepSeekThinkingHasASignature(t *testing.T) {
	f := &fake{t: t, ctype: "application/json", reply: anthropicWholeOK, refuse: openRouterRefuseAnthropic}
	up := setup(t, provider.Anthropic, f)
	if err := provider.Save(provider.Provider{ID: "openrouter", Name: "OpenRouter", Key: "k", Models: []string{"deepseek/deepseek-v4.1-flash-1017"}, Anthropic: up.URL}); err != nil {
		t.Fatal(err)
	}
	srv := New()
	code, body := postTo(t, srv, "/v1/messages", strings.Replace(claudeTurnsForDeepSeek, "%s", "openrouter/deepseek/deepseek-v4.1-flash-1017", 1))
	if code != 200 || f.calls != 1 {
		t.Fatalf("messages: %d after %d calls: %s", code, f.calls, body)
	}
	ms := sentAnthropicMessages(t, f.got)
	sameJSON(t, ms[1], `{"role":"assistant","content":[{"type":"thinking","thinking":" ","signature":""},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}`)
	sameJSON(t, ms[3], `{"role":"assistant","content":[{"type":"thinking","thinking":"Read main.go.","signature":"EqQBCkgIBxABGAIiQL"},{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"cat main.go"}}]}`)

	// a Chat client's request goes on as a stream
	f.calls, f.ctype, f.reply = 0, "", sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m1","usage":{"input_tokens":7}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"12:01"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`data: {"type":"message_stop"}`)
	chat := `{"model":"openrouter/deepseek/deepseek-v4.1-flash-1017","stream":false,"tools":[{"type":"function","function":{"name":"get_time","parameters":{"type":"object","properties":{}}}}],"messages":[
		{"role":"user","content":"what time is it? twice"},
		{"role":"assistant","content":null,"tool_calls":[` + timeCall + `]},
		{"role":"tool","tool_call_id":"call_1","content":"12:00"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"get_time","arguments":"{}"}}],"reasoning_content":"Once more."},
		{"role":"tool","tool_call_id":"call_2","content":"12:01"}]}`
	if code, body := postTo(t, srv, "/v1/chat/completions", chat); code != 200 || f.calls != 1 {
		t.Fatalf("chat: %d after %d calls: %s", code, f.calls, body)
	}
	if !strings.Contains(string(f.got), `{"type":"thinking","thinking":"Once more.","signature":""}`) {
		t.Errorf("the client's reasoning went without a signature: %s", f.got)
	}
}

// Translated onto DeepSeek's Anthropic endpoint (a Chat client, the
// provider giving only a Messages URL), a tool call with no thinking goes
// after a thinking block holding a space, and one with DeepSeek's own
// reasoning_content keeps its text (#1611); another model is sent neither.
func TestTranslatedDeepSeekAnthropicToolTurn(t *testing.T) {
	f := &fake{t: t, refuse: deepseekRefuseAnthropic, reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m1","usage":{"input_tokens":7}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"12:01"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`data: {"type":"message_stop"}`)}
	up := setup(t, provider.Anthropic, f)
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DS", Key: "k", Models: []string{"deepseek-flash", "glm-5.2"}, Anthropic: up.URL}); err != nil {
		t.Fatal(err)
	}
	srv := New()
	chat := `{"model":"%s","stream":false,"reasoning_effort":"high","tools":[{"type":"function","function":{"name":"get_time","parameters":{"type":"object","properties":{}}}}],"messages":[
		{"role":"user","content":"what time is it? twice"},
		{"role":"assistant","content":null,"tool_calls":[` + timeCall + `]},
		{"role":"tool","tool_call_id":"call_1","content":"12:00"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"get_time","arguments":"{}"}}],"reasoning_content":"Once more."},
		{"role":"tool","tool_call_id":"call_2","content":"12:01"}]}`
	code, body := postTo(t, srv, "/v1/chat/completions", strings.Replace(chat, "%s", "ds/deepseek-flash", 1))
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls: %s", code, f.calls, body)
	}
	var thinking []string
	for _, raw := range sentAnthropicMessages(t, f.got) {
		var m struct {
			Role    string `json:"role"`
			Content []aBlock
		}
		json.Unmarshal(raw, &m)
		for _, b := range m.Content {
			if b.Type == "thinking" {
				thinking = append(thinking, b.Thinking)
			}
		}
	}
	if got := strings.Join(thinking, "|"); got != " |Once more." {
		t.Errorf("thinking sent %q, want %q: %s", got, " |Once more.", f.got)
	}

	f.refuse = nil
	if code, body := postTo(t, srv, "/v1/chat/completions", strings.Replace(chat, "%s", "ds/glm-5.2", 1)); code != 200 {
		t.Fatalf("glm: %d %s", code, body)
	}
	if strings.Contains(string(f.got), `"type":"thinking"`) {
		t.Errorf("glm was given thinking: %s", f.got)
	}
}

// withThinkingOnToolTurns leaves a body with nothing to change byte for
// byte.
func TestWithThinkingOnToolTurnsUnchanged(t *testing.T) {
	for _, b := range []string{
		`{"model":"deepseek-flash","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"deepseek-flash",  "messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"x"},{"type":"tool_use","id":"t","name":"n","input":{}}]}]}`,
		`{"messages":[{"role":"assistant","content":"plain"}]}`,
	} {
		if got := string(withThinkingOnToolTurns([]byte(b))); got != b {
			t.Errorf("%s\nbecame %s", b, got)
		}
	}
	if anthropicReplaysThinking("api.anthropic.com", "deepseek-flash") || anthropicReplaysThinking("api.deepseek.com", "claude-sonnet-4-6") || anthropicReplaysThinking("api.moonshot.ai", "kimi-k2") {
		t.Error("replays thinking outside DeepSeek")
	}
	if !anthropicReplaysThinking("api.deepseek.com", "deepseek-flash") || !anthropicReplaysThinking("relay.example", "DeepSeek-V4-Pro") {
		t.Error("DeepSeek not replayed")
	}
}

// OpenRouter's Anthropic endpoint takes a turn's effort, the output_config
// Claude Code 2.1.302 puts on a {role: "system"} message, as a reasoning
// configuration_update, and turns it away on DeepSeek and GLM with the
// 400 #3696 quotes. uni-switch asks again without it (withoutTurnEffort), and
// from then on sends OpenRouter none; the request's own effort and the
// system message's text stay. On DeepSeek the tool turn's thinking says
// its signature too (#2883), or the second try is refused for that.
func TestOpenRouterTurnEffortLeftOut(t *testing.T) {
	for _, model := range []string{"deepseek/deepseek-v4.1-flash-1017", "z-ai/glm-5.3-flash"} {
		t.Run(model, func(t *testing.T) {
			f := &fake{t: t, ctype: "application/json", reply: anthropicWholeOK, refuse: func(body []byte) (int, string) {
				var q struct {
					Messages []map[string]json.RawMessage `json:"messages"`
				}
				json.Unmarshal(body, &q)
				for _, m := range q.Messages {
					if _, ok := m["output_config"]; ok {
						return 400, `{"error":{"type":"invalid_request_error","message":"Mid-conversation reasoning effort (configuration_update) is not supported on ` + model + `"}}`
					}
				}
				return openRouterRefuseAnthropic(body)
			}}
			up := setup(t, provider.Anthropic, f)
			if err := provider.Save(provider.Provider{ID: "openrouter", Name: "OpenRouter", Key: "k", Models: []string{model}, Anthropic: up.URL}); err != nil {
				t.Fatal(err)
			}
			srv := New()
			body := `{"model":"openrouter/` + model + `","max_tokens":64,"stream":false,"output_config":{"effort":"high"},
				"tools":[{"name":"Bash","input_schema":{"type":"object","properties":{}}}],
				"messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},
				{"role":"system","output_config":{"effort":"medium"},"content":[{"type":"text","text":"# Environment <cwd>"}]},
				{"role":"user","content":"ok"}]}`
			code, out := postTo(t, srv, "/v1/messages", body)
			if code != 200 || f.calls != 2 {
				t.Fatalf("%d after %d calls, want 200 on the second: %s", code, f.calls, out)
			}
			check := func() {
				t.Helper()
				if oc := gjson.GetBytes(f.got, "output_config.effort").String(); oc != "high" {
					t.Errorf("output_config.effort = %q, want the request's own", oc)
				}
				ms := sentAnthropicMessages(t, f.got)
				if len(ms) != 5 {
					t.Fatalf("%d messages sent, want the system message kept: %s", len(ms), f.got)
				}
				sameJSON(t, ms[3], `{"role":"system","content":[{"type":"text","text":"# Environment <cwd>"}]}`)
			}
			check()
			// learned: the next request goes as OpenRouter takes it at once
			f.calls = 0
			if code, out = postTo(t, srv, "/v1/messages", body); code != 200 || f.calls != 1 {
				t.Fatalf("%d after %d calls, want 200 on the first: %s", code, f.calls, out)
			}
			check()
		})
	}
}
