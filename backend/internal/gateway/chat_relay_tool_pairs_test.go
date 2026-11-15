package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// kimiRefusePairs answers as api.kimi.com/coding/v1 did on 2026-10-26 a
// Chat request whose tool exchange doesn't pair: a tool message with no
// call before it, and an assistant message's call that the tool messages
// right after it don't answer. Kimi names a call by its function and
// place ("read_file:0") whatever id the client gave it; here it is the
// client's.
func kimiRefusePairs(body []byte) (int, string) {
	var q struct {
		Messages []map[string]any `json:"messages"`
	}
	json.Unmarshal(body, &q)
	var pending []string
	answered := map[string]bool{}
	unanswered := func() string {
		var left []string
		for _, id := range pending {
			if !answered[id] {
				left = append(left, id)
			}
		}
		pending, answered = nil, map[string]bool{}
		return strings.Join(left, ", ")
	}
	refuse := func(left string) (int, string) {
		return 400, fmt.Sprintf(`{"error":{"message":"an assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. The following tool_call_ids did not have response messages: %s","type":"invalid_request_error"}}`, left)
	}
	for _, m := range q.Messages {
		if m["role"] == "tool" {
			id, _ := m["tool_call_id"].(string)
			if !strings.Contains(" "+strings.Join(pending, " ")+" ", " "+id+" ") {
				return 400, `{"error":{"message":"tool_call_id  is not found","type":"invalid_request_error"}}`
			}
			answered[id] = true
			continue
		}
		if left := unanswered(); left != "" {
			return refuse(left)
		}
		calls, _ := m["tool_calls"].([]any)
		for _, c := range calls {
			id, _ := c.(map[string]any)["id"].(string)
			pending = append(pending, id)
		}
	}
	if left := unanswered(); left != "" {
		return refuse(left)
	}
	return 0, ""
}

func readCall(id string) string {
	return `{"id":"` + id + `","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}`
}

// Paidaixinbao (X): Qoder's chat, its model switched to Kimi Code, forked
// at an earlier turn, was refused on every prompt while a new chat worked.
// A Chat agent's history can hold a call no tool message answers (a fork
// or a stopped reply cut the turn at its call), an answer whose call is
// gone, a user message between a call and its answer, or a turn's calls
// split over two assistant messages, and Kimi turns the whole request
// away for each, as it does on its real API. Relayed to Kimi, each one
// goes paired and is answered.
func TestChatRelayPairsToolsForKimi(t *testing.T) {
	f := &fake{t: t, reply: chatWholeOK, ctype: "application/json", refuse: kimiRefusePairs}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "kimi-code-cn", Name: "Kimi Code (China)", Key: "k", Models: []string{"k3"},
		Preset: "kimi-code-cn", Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	srv := New()
	user := func(s string) string { return `{"role":"user","content":"` + s + `"}` }
	calls := func(ids ...string) string {
		var cs []string
		for _, id := range ids {
			cs = append(cs, readCall(id))
		}
		return `{"role":"assistant","content":"","tool_calls":[` + strings.Join(cs, ",") + `]}`
	}
	tool := func(id, out string) string {
		return `{"role":"tool","tool_call_id":"` + id + `","content":"` + out + `"}`
	}
	for _, c := range []struct{ name, turns, shape, has string }{
		{"call unanswered", user("read a.txt") + "," + calls("a") + "," + user("go on"),
			"user assistant(a) tool(a) user", "interrupted"},
		{"one of two calls answered", user("read a.txt") + "," + calls("a", "b") + "," + tool("a", "hello") + "," + user("go on"),
			"user assistant(a,b) tool(a) tool(b) user", "hello"},
		{"answer with no call", user("read a.txt") + "," + tool("a", "hello") + "," + user("go on"),
			"user user", "hello"},
		{"user between call and answer", user("read a.txt") + "," + calls("a") + "," + user("wait") + "," + tool("a", "hello") + "," + user("go on"),
			"user assistant(a) tool(a) user user", "hello"},
		{"calls split over two messages", user("read a.txt") + "," + calls("a") + "," + calls("b") + "," + tool("a", "1") + "," + tool("b", "2") + "," + user("go on"),
			"user assistant(a,b) tool(a) tool(b) user", "go on"},
		{"answer in parts with no call", user("read a.txt") + `,{"role":"tool","tool_call_id":"a","content":[{"type":"text","text":"hello in parts"}]},` + user("go on"),
			"user user", "hello in parts"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.calls = 0
			code, body := postTo(t, srv, "/v1/chat/completions", `{"model":"kimi-code-cn/k3","stream":false,"messages":[`+c.turns+`]}`)
			if code != 200 || f.calls != 1 {
				t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
			}
			var msgs []map[string]any
			for _, raw := range sentMessages(t, f.got) {
				var m map[string]any
				json.Unmarshal(raw, &m)
				msgs = append(msgs, m)
			}
			if shape := relayTrace(msgs); shape != c.shape {
				t.Errorf("sent %s, want %s: %s", shape, c.shape, f.got)
			}
			if !strings.Contains(string(f.got), c.has) {
				t.Errorf("sent without %q: %s", c.has, f.got)
			}
			if c.name == "user between call and answer" && !bytes.Contains(f.got, []byte(`"wait\n\nhello"`)) {
				t.Errorf("the late answer didn't join the user's message: %s", f.got)
			}
		})
	}
}

// relayTrace is the roles of a sent Chat request's messages, with their
// tool call ids, as chatTrace has them.
func relayTrace(msgs []map[string]any) string {
	var b strings.Builder
	for _, m := range msgs {
		role, _ := m["role"].(string)
		b.WriteString(" " + role)
		if calls, ok := m["tool_calls"].([]any); ok {
			var ids []string
			for _, c := range calls {
				id, _ := c.(map[string]any)["id"].(string)
				ids = append(ids, id)
			}
			b.WriteString("(" + strings.Join(ids, ",") + ")")
		}
		if id, ok := m["tool_call_id"].(string); ok {
			b.WriteString("(" + id + ")")
		}
	}
	return strings.TrimSpace(b.String())
}

// A relayed body whose exchange pairs goes byte for byte, answers out of
// the calls' order included (Kimi takes them), and so does one with no
// tools at all.
func TestChatRelayPairedToolsUntouched(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","temperature":0.70,"messages":[{"role":"user","content":"a <b> & c"},{"role":"assistant","content":null,"tool_calls":[` + readCall("a") + `,` + readCall("b") + `]},{"role":"tool","tool_call_id":"b","content":"2"},{"role":"tool","tool_call_id":"a","content":"1"},{"role":"assistant","content":"done"},{"role":"user","content":"go"}]}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello","n":1.50},{"role":"user","content":"go"}]}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"f","parameters":{}}}]}`,
	} {
		if got := toolPairsOut([]byte(body)); string(got) != body {
			t.Errorf("a paired body was rewritten:\n%s\n%s", body, got)
		}
	}
}
