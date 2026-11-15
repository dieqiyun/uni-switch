package gateway

import (
	"encoding/json"
	"fmt"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// deepseekRefuseToolTurns answers as DeepSeek's Chat API does in thinking
// mode (checked against api.deepseek.com, October 2026): a tool call made
// since the user's last message that comes back without reasoning_content
// turns the whole request away, as does a reply to a tool result without
// it, while an empty one, another reply's, or one
// from before the user's last message missing are taken.
func deepseekRefuseToolTurns(body []byte) (int, string) {
	var q struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &q)
	last := -1
	for i, m := range q.Messages {
		if string(m["role"]) == `"user"` {
			last = i
		}
	}
	afterTool := false
	for _, m := range q.Messages[last+1:] {
		// a reply to a tool result too (checked 2026-10-12, #1822)
		if string(m["role"]) == `"assistant"` && (len(m["tool_calls"]) > 0 || afterTool) && m["reasoning_content"] == nil {
			return 400, `{"error":{"message":"The ` + "`reasoning_content`" + ` in the thinking mode must be passed back to the API. (request_id: 06048513-d7c7-4cdf-9998-47bfc20484e6)","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`
		}
		afterTool = string(m["role"]) == `"tool"`
	}
	return 0, ""
}

const timeCall = `{"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{}"}}`

// the issue's request: OpenCode continuing a turn whose tool calls it has
// no thinking for (one another model made), and one that has its own
const toolTurnsNoReasoning = `{"model":"%s","stream":false,"tools":[{"type":"function","function":{"name":"get_time","parameters":{"type":"object","properties":{}}}}],"messages":[
	{"role":"user","content":"hi"},
	{"role":"assistant","content":"Hello!"},
	{"role":"user","content":"what time is it? twice"},
	{"role":"assistant","content":null,"tool_calls":[` + timeCall + `]},
	{"role":"tool","tool_call_id":"call_1","content":"12:00"},
	{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"get_time","arguments":"{}"}}],"reasoning_content":"Once more."},
	{"role":"tool","tool_call_id":"call_2","content":"12:01"},
	{"role":"assistant","content":null,"tool_calls":[{"id":"call_3","type":"function","function":{"name":"get_time","arguments":"{}"}}],"reasoning":"OpenRouter's field."},
	{"role":"tool","tool_call_id":"call_3","content":"12:02"}]}`

func chatProvider(t *testing.T, up string, p provider.Provider) {
	t.Helper()
	p.Name, p.Key, p.Chat = p.ID, "k", up+"/v1"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
}

// Mistral, which turns reasoning_content away (#494), is given none for a
// tool call without thinking.
func TestChatToolTurnWithoutReasoningNotForMistral(t *testing.T) {
	f := &fake{t: t, reply: chatWholeOK, ctype: "application/json", refuse: mistralRefuseMessages}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "mistral", Preset: "mistral", Models: []string{"mistral-medium-3.5"}})
	code, body := postTo(t, New(), "/v1/chat/completions", `{"model":"mistral/mistral-medium-3.5","stream":false,"messages":[
		{"role":"user","content":"what time is it?"},
		{"role":"assistant","content":null,"tool_calls":[`+timeCall+`]},
		{"role":"tool","tool_call_id":"call_1","content":"12:00"}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls: %s", code, f.calls, body)
	}
	sameJSON(t, sentMessages(t, f.got)[1], `{"role":"assistant","content":null,"tool_calls":[`+timeCall+`]}`)
}

// Translated to Chat (Codex on Responses, here a turn whose call came with
// no reasoning item: another model's), a DeepSeek model's tool call goes
// with reasoning_content all the same (#1462); a reply with no tool calls
// goes without.
func TestTranslatedToolTurnWithoutReasoning(t *testing.T) {
	f := &fake{t: t, refuse: deepseekRefuseToolTurns, reply: `data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","content":"It is 12:00."},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}` + "\n\ndata: [DONE]\n\n"}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "ds", Models: []string{"deepseek-v4.1-flash"}})
	code, body := postTo(t, New(), "/v1/responses", `{"model":"ds/deepseek-v4.1-flash","stream":false,"tools":[{"type":"function","name":"get_time","parameters":{"type":"object","properties":{}}}],"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello!"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"what time is it?"}]},
		{"type":"function_call","call_id":"call_1","name":"get_time","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":"12:00"}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls: %s", code, f.calls, body)
	}
	ms := sentMessages(t, f.got)
	var reply, call map[string]any
	for _, raw := range ms {
		var m map[string]any
		json.Unmarshal(raw, &m)
		if m["role"] != "assistant" {
			continue
		}
		if m["tool_calls"] != nil {
			call = m
		} else {
			reply = m
		}
	}
	if call == nil || call["reasoning_content"] != " " {
		t.Errorf("the tool call went without reasoning: %s", f.got)
	}
	if _, ok := reply["reasoning_content"]; reply == nil || ok {
		t.Errorf("the reply was given reasoning: %s", f.got)
	}
}

// selfHostedRefuseDuplicate answers as the reporter's self-hosted DeepSeek
// server does: it reads reasoning and reasoning_content as one field, so a
// message carrying both is turned away, and DeepSeek's own rule that a
// tool turn needs its thinking back holds too.
func selfHostedRefuseDuplicate(body []byte) (int, string) {
	var q struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &q)
	for i, m := range q.Messages {
		if m["reasoning"] != nil && m["reasoning_content"] != nil {
			return 400, fmt.Sprintf(`{"error":{"message":"Failed to deserialize the JSON body into the target type: messages[%d]: duplicate field `+"`reasoning`"+`","type":"invalid_request_error"}}`, i)
		}
	}
	return deepseekRefuseToolTurns(body)
}

// ArvinCharl on X (v0.1.1560): pi on a self-hosted DeepSeek through the
// Chat path keeps the thinking the server gave in reasoning and sends it
// back there. uni-switch gave the tool turn reasoning_content with the same
// text and left reasoning in, and the server refused "duplicate field
// reasoning". The thinking goes once, in reasoning_content; a message with
// no tool turn to mend keeps its reasoning, and a body with no tool calls
// goes byte for byte.
func TestSelfHostedDeepSeekGetsReasoningOnce(t *testing.T) {
	f := &fake{t: t, reply: chatWholeOK, ctype: "application/json", refuse: selfHostedRefuseDuplicate}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "ds-local", Models: []string{"deepseek-v4"}})
	srv := New()
	code, body := postTo(t, srv, "/v1/chat/completions", `{"model":"ds-local/deepseek-v4","stream":false,"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object","properties":{}}}}],"messages":[
		{"role":"user","content":"Read note.txt."},
		{"role":"assistant","content":"","reasoning":"I should read the file first.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"note.txt\"}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"hello"},
		{"role":"assistant","content":"It says hello.","reasoning":"The file says hello.","reasoning_details":[{"type":"reasoning.encrypted","data":"gAAAA"}]},
		{"role":"user","content":"Thanks."}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
	}
	ms := sentMessages(t, f.got)
	sameJSON(t, ms[1], `{"role":"assistant","content":"","reasoning_content":"I should read the file first.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"note.txt\"}"}}]}`)
	// a reply to a tool result is given it as well (#1822); its
	// reasoning_details, not another name for the text, stays
	sameJSON(t, ms[3], `{"role":"assistant","content":"It says hello.","reasoning_content":"The file says hello.","reasoning_details":[{"type":"reasoning.encrypted","data":"gAAAA"}]}`)

	// with no tool calls, the body goes as it was sent, reasoning and all
	f.refuse, f.calls = nil, 0
	plain := `{"model":"deepseek-v4","stream":false,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"Hello!","reasoning":"Greet back."},{"role":"user","content":"bye"}]}`
	if got := withReasoningOnToolTurns([]byte(plain)); string(got) != plain {
		t.Errorf("a body with no tool calls changed:\n%s", got)
	}
}

// Kimi, given reasoning_details' thinking as reasoning_content (#1765),
// is not also sent the message's reasoning with the same text.
func TestKimiReasoningDetailsWithReasoningGoOnce(t *testing.T) {
	m := `{"messages":[{"role":"assistant","content":"91 = 7 x 13.","reasoning":"7*13=91.","reasoning_details":[{"type":"reasoning.text","text":"7*13=91.","index":0}]}]}`
	sameJSON(t, sentMessages(t, reasoningDetailsAsContent([]byte(m)))[0], `{"role":"assistant","content":"91 = 7 x 13.","reasoning_content":"7*13=91."}`)
}
