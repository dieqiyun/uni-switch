package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2611's minimal case, as RunMintOn sent it to WorkBuddy AI: a call Pi
// kept with an empty id (an OpenCode Zen free DeepSeek turn), its result
// with an empty tool_call_id, then the user. A turn of the same session
// whose calls came with ids follows, so a call's own id is seen to go as
// it was.
const emptyCallIDTurns = `{"model":"%s","stream":true,"messages":[
	{"role":"user","content":"look at the toggle"},
	{"role":"assistant","content":null,"tool_calls":[{"id":"","type":"function","function":{"name":"bash","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"","content":"out"},
	{"role":"assistant","content":null,"reasoning_content":"Let me find providerSwitch(p)…","tool_calls":[{"id":"call_00_2omk8cv76gjjovgj3caocaif","type":"function","function":{"name":"bash","arguments":"{}"}},{"id":"call_01_h9qf0fe96gtektv6efu80mjt","type":"function","function":{"name":"bash","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"call_00_2omk8cv76gjjovgj3caocaif","content":"a"},
	{"role":"tool","tool_call_id":"call_01_h9qf0fe96gtektv6efu80mjt","content":"b"},
	{"role":"user","content":"你好"}]}`

// workBuddyPairing answers a streamed Chat request as WorkBuddy AI did
// for #2611: an empty call id, or a result naming no call of the turn
// before it, turns the request away.
func workBuddyPairing(body []byte) (int, string) {
	var q struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role       string  `json:"role"`
			ToolCallID *string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &q)
	refuse := `{"code":null,"message":"WorkBuddy AI: the request parameters were rejected by the model provider","param":null,"type":"invalid_request_error"}`
	if !q.Stream {
		return 0, ""
	}
	calls := map[string]bool{}
	for _, m := range q.Messages {
		switch m.Role {
		case "assistant":
			calls = map[string]bool{}
			for _, c := range m.ToolCalls {
				if c.ID == "" || calls[c.ID] {
					return 400, refuse
				}
				calls[c.ID] = true
			}
		case "tool":
			if m.ToolCallID == nil || !calls[*m.ToolCallID] {
				return 400, refuse
			}
		}
	}
	return 0, ""
}

const chatStreamHello = "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"m1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你好\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

// sentCallIDs reads the ids of a sent Chat request's calls and results in
// order, "call:<id>" and "tool:<id>".
func sentCallIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var ids []string
	for _, raw := range sentMessages(t, body) {
		var m struct {
			ToolCallID *string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		}
		json.Unmarshal(raw, &m)
		for _, c := range m.ToolCalls {
			ids = append(ids, "call:"+c.ID)
		}
		if m.ToolCallID != nil {
			ids = append(ids, "tool:"+*m.ToolCallID)
		}
	}
	return ids
}

// checkMadeIDs checks #2611's request went with an id made for its empty
// call, the same on its result, and the next turn's own ids as they were.
func checkMadeIDs(t *testing.T, ids []string) string {
	t.Helper()
	if len(ids) != 6 || ids[0] == "call:" || ids[1] != "tool:"+strings.TrimPrefix(ids[0], "call:") ||
		strings.Join(ids[2:], " ") != "call:call_00_2omk8cv76gjjovgj3caocaif call:call_01_h9qf0fe96gtektv6efu80mjt tool:call_00_2omk8cv76gjjovgj3caocaif tool:call_01_h9qf0fe96gtektv6efu80mjt" {
		t.Fatalf("sent ids %v", ids)
	}
	return ids[0]
}

// A Chat request relayed to a Chat upstream carries a call kept with an
// empty id with one made from its place, and its result with the same
// (#2611): WorkBuddy AI turned every later request of the session away.
// The made id is the same on the next request, so the history's prefix,
// and the upstream's cache of it, stays as it was.
func TestEmptyCallIDRelayedWithOne(t *testing.T) {
	f := &fake{t: t, reply: chatStreamHello, refuse: workBuddyPairing}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "wb", Models: []string{"deepseek-v4.1-flash"}})
	srv := New()
	var first string
	for i := range 2 {
		code, body := postTo(t, srv, "/v1/chat/completions", fmt.Sprintf(emptyCallIDTurns, "wb/deepseek-v4.1-flash"))
		if code != 200 {
			t.Fatalf("request %d: %d %s", i, code, body)
		}
		id := checkMadeIDs(t, sentCallIDs(t, f.got))
		if i == 0 {
			first = id
		} else if id != first {
			t.Fatalf("the made id changed between requests: %s, then %s", first, id)
		}
	}
}

// Translated, a Chat request's empty call id goes to a Messages upstream
// as a tool_use id its result names, and to a Responses upstream as a
// call_id its output names: neither takes an empty one.
func TestEmptyCallIDTranslatedWithOne(t *testing.T) {
	t.Run("messages", func(t *testing.T) {
		f := &fake{t: t, reply: sse("event: message_start\n"+`data: {"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
			"event: message_delta\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			"event: message_stop\n"+`data: {"type":"message_stop"}`)}
		setup(t, provider.Anthropic, f)
		code, body := postTo(t, New(), "/v1/chat/completions", fmt.Sprintf(emptyCallIDTurns, "fake/m1"))
		if code != 200 {
			t.Fatalf("%d %s", code, body)
		}
		var q struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(f.got, &q)
		var ids []string
		for _, m := range q.Messages {
			var blocks []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				ToolUseID string `json:"tool_use_id"`
			}
			json.Unmarshal(m.Content, &blocks)
			for _, b := range blocks {
				switch b.Type {
				case "tool_use":
					ids = append(ids, "call:"+b.ID)
				case "tool_result":
					ids = append(ids, "tool:"+b.ToolUseID)
				}
			}
		}
		checkMadeIDs(t, ids)
	})
	t.Run("responses", func(t *testing.T) {
		f := &fake{t: t, reply: sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
			`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
		setup(t, provider.Responses, f)
		code, body := postTo(t, New(), "/v1/chat/completions", fmt.Sprintf(emptyCallIDTurns, "fake/m1"))
		if code != 200 {
			t.Fatalf("%d %s", code, body)
		}
		var q struct {
			Input []struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
			} `json:"input"`
		}
		json.Unmarshal(f.got, &q)
		var ids []string
		for _, it := range q.Input {
			switch it.Type {
			case "function_call":
				ids = append(ids, "call:"+it.CallID)
			case "function_call_output":
				ids = append(ids, "tool:"+it.CallID)
			}
		}
		checkMadeIDs(t, ids)
	})
}

// A body whose calls all have ids goes byte for byte, and results with
// no id beyond the calls left without one are left as they came.
func TestChatCallIDsLeavesWhatHasIDs(t *testing.T) {
	body := []byte(strings.ReplaceAll(fmt.Sprintf(emptyCallIDTurns, "m"), `"id":""`, `"id":"call_x"`))
	body = []byte(strings.Replace(string(body), `"tool_call_id":""`, `"tool_call_id":"call_x"`, 1))
	if got := chatCallIDs(body); string(got) != string(body) {
		t.Fatalf("changed:\n%s", got)
	}
	// two calls with none, answered without ids in their order; a third
	// result with none has no call left to take
	two := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"a","arguments":"{}"}},{"id":null,"type":"function","function":{"name":"b","arguments":"{}"}}]},{"role":"tool","content":"1"},{"role":"tool","tool_call_id":"","content":"2"},{"role":"tool","tool_call_id":"","content":"3"}]}`)
	ids := sentCallIDs(t, chatCallIDs(two))
	if len(ids) != 5 || ids[0] == ids[1] || ids[2] != "tool:"+strings.TrimPrefix(ids[0], "call:") || ids[3] != "tool:"+strings.TrimPrefix(ids[1], "call:") || ids[4] != "tool:" {
		t.Fatalf("ids %v", ids)
	}
}

// A relayed Chat stream whose upstream left a tool call's id out (as
// OpenCode Zen's free DeepSeek sometimes does, #2611) reaches the agent
// with one on the call's first fragment, so the agent never keeps "";
// a later fragment's empty id is left out, and a call with its own id
// keeps it.
func TestStreamedCallGivenAnID(t *testing.T) {
	reply := sse(
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"type":"function","function":{"name":"bash","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","function":{"arguments":"{\"command\":\"ls\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"tool_calls":[{"index":2,"id":"call_02_own","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`)
	f := &fake{t: t, reply: reply}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "zen", Models: []string{"deepseek-v4.1-flash-free"}})
	code, body := postTo(t, New(), "/v1/chat/completions", `{"model":"zen/deepseek-v4.1-flash-free","stream":true,"messages":[{"role":"user","content":"ls"}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var frags []map[string]any
	for _, ev := range events(body) {
		for _, c := range ev["choices"].([]any) {
			d, _ := c.(map[string]any)["delta"].(map[string]any)
			calls, _ := d["tool_calls"].([]any)
			for _, tc := range calls {
				frags = append(frags, tc.(map[string]any))
			}
		}
	}
	if len(frags) != 4 {
		t.Fatalf("fragments %v\n%s", frags, body)
	}
	id0, _ := frags[0]["id"].(string)
	id1, _ := frags[2]["id"].(string)
	if !strings.HasPrefix(id0, "call_") || !strings.HasPrefix(id1, "call_") || id0 == id1 {
		t.Fatalf("first fragments' ids %q, %q\n%s", id0, id1, body)
	}
	if _, has := frags[1]["id"]; has {
		t.Fatalf("a later fragment kept its empty id\n%s", body)
	}
	if frags[3]["id"] != "call_02_own" {
		t.Fatalf("a call's own id went as %v", frags[3]["id"])
	}
}

// A whole reply's call with no id is given one too.
func TestWholeReplyCallGivenAnID(t *testing.T) {
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"c1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "zen", Models: []string{"deepseek-v4.1-flash-free"}})
	code, body := postTo(t, New(), "/v1/chat/completions", `{"model":"zen/deepseek-v4.1-flash-free","stream":false,"messages":[{"role":"user","content":"ls"}]}`)
	var got struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct{ ID string } `json:"tool_calls"`
			}
		}
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || len(got.Choices) != 1 || len(got.Choices[0].Message.ToolCalls) != 1 ||
		!strings.HasPrefix(got.Choices[0].Message.ToolCalls[0].ID, "call_") {
		t.Fatalf("%d %s", code, body)
	}
}
