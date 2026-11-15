package gateway

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// wantInterrupted is the synthetic result the other paths make for a call
// whose turn was interrupted (pairToolMessages, pairToolItems).
const wantInterrupted = "[The result of this tool call is unavailable: the turn was interrupted.]"

// aBlockProbe is one content block of a request built for a Messages
// upstream, as the tests read it back.
type aBlockProbe struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// aMessageProbe is one message of it.
type aMessageProbe struct {
	Role    string        `json:"role"`
	Content []aBlockProbe `json:"content"`
}

// messagesTrace renders the message sequence of a request built for a
// Messages upstream: roles with the calls made and the results answered,
// e.g. "user assistant(A) user[A] user".
func messagesTrace(t *testing.T, r *Request) (string, []aMessageProbe) {
	t.Helper()
	body := buildAnthropic(r, "claude-sonnet-4-5")
	var q struct {
		Messages []aMessageProbe `json:"messages"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		t.Fatalf("built request does not parse: %v", err)
	}
	var b strings.Builder
	for _, m := range q.Messages {
		b.WriteString(" " + m.Role)
		var calls, results []string
		for _, c := range m.Content {
			switch c.Type {
			case "tool_use":
				calls = append(calls, c.ID)
			case "tool_result":
				results = append(results, c.ToolUseID)
			}
		}
		if len(calls) > 0 {
			b.WriteString("(" + strings.Join(calls, ",") + ")")
		}
		if len(results) > 0 {
			b.WriteString("[" + strings.Join(results, ",") + "]")
		}
	}
	return strings.TrimSpace(b.String()), q.Messages
}

// anthropicValid reports whether a built message sequence passes what
// Anthropic's Messages API validates: every tool_result names a call of the
// assistant message it follows, and every call is answered by the results
// of that message.
func anthropicValid(msgs []aMessageProbe) bool {
	pending := map[string]bool{}
	for _, m := range msgs {
		if m.Role == "assistant" {
			pending = map[string]bool{}
			for _, c := range m.Content {
				if c.Type == "tool_use" {
					pending[c.ID] = true
				}
			}
			continue
		}
		for _, c := range m.Content {
			if c.Type != "tool_result" {
				continue
			}
			if !pending[c.ToolUseID] {
				return false // unexpected tool_use_id found in tool_result blocks
			}
			delete(pending, c.ToolUseID)
		}
		if len(pending) > 0 {
			return false // tool_use ids were found without tool_result blocks
		}
	}
	return true
}

// messagesRefused is Anthropic's refusal of a broken tool exchange, as its
// API answers it and a relay serving Claude over Messages passes it on.
func messagesRefused(at int, msg string) string {
	b, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{
		"type": "invalid_request_error", "message": "messages." + strconv.Itoa(at) + ": " + msg}})
	return string(b)
}

// validateExchange refuses a Messages request the way Anthropic's own does
// over a broken tool exchange, with its documented 400s.
func validateExchange(body []byte) (int, string) {
	var q struct {
		Messages []aMessageProbe `json:"messages"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		return 400, messagesRefused(0, "could not parse the request")
	}
	pending := map[string]bool{}
	for i, m := range q.Messages {
		if m.Role == "assistant" {
			for _, c := range m.Content {
				if c.Type == "tool_use" {
					pending[c.ID] = true
				}
			}
			continue
		}
		for _, c := range m.Content {
			if c.Type != "tool_result" {
				continue
			}
			if !pending[c.ToolUseID] {
				return 400, messagesRefused(i, "unexpected `tool_use_id` found in `tool_result` blocks: "+c.ToolUseID+". Each `tool_result` block must have a corresponding `tool_use` block in the previous message.")
			}
			delete(pending, c.ToolUseID)
		}
		for id := range pending {
			return 400, messagesRefused(i, "`tool_use` ids were found without `tool_result` blocks immediately after: "+id+". Each `tool_use` block must have a corresponding `tool_result` block in the next message.")
		}
	}
	for id := range pending {
		return 400, messagesRefused(len(q.Messages), "`tool_use` ids were found without `tool_result` blocks immediately after: "+id+". Each `tool_use` block must have a corresponding `tool_result` block in the next message.")
	}
	return 0, ""
}

// An interrupted turn leaves its call without a result: the synthetic one
// goes into the user message that follows, before what the user said, as
// pairToolMessages and pairToolItems make for the Chat and Responses paths
// (#554, #1341). Anthropic refuses the call as it was — "messages.2:
// `tool_use` ids were found without `tool_result` blocks immediately
// after" — and a relay serving Claude over Messages passes it on.
func TestMessagesUnansweredCallGetsSyntheticResult(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run it"}}},
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "go on"}}},
	}}
	trace, msgs := messagesTrace(t, r)
	want := "user assistant(A) user[A]"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !anthropicValid(msgs) {
		t.Fatal("sequence fails Anthropic's tool exchange validation")
	}
	if got := msgs[2].Content[0]; got.Type != "tool_result" || got.ToolUseID != "A" || string(got.Content) != `"`+wantInterrupted+`"` || !got.IsError {
		t.Fatalf("the synthetic result is not what the other paths make: %+v %s", got, got.Content)
	}
	if got := msgs[2].Content[1]; got.Type != "text" || got.Text != "go on" {
		t.Fatalf("what the user said is not kept after it: %+v", got)
	}
}

// A result answering no call the request carries — its call went with an
// earlier request, was closed already, or was never there — becomes a text
// block, as a Chat upstream's becomes a user message. Anthropic refuses it
// as it is: "unexpected `tool_use_id` found in `tool_result` blocks".
func TestMessagesOrphanToolResultBecomesText(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run it"}}},
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "user", Parts: []Part{result("A", "ok"), result("gone", "late")}},
	}}
	trace, msgs := messagesTrace(t, r)
	want := "user assistant(A) user[A]"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !anthropicValid(msgs) {
		t.Fatal("sequence fails Anthropic's tool exchange validation")
	}
	var texts []string
	for _, c := range msgs[2].Content {
		if c.Type == "tool_result" && c.ToolUseID == "gone" {
			t.Fatalf("the orphan result is still a tool_result: %+v", c)
		}
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) == 0 || texts[0] != "late" {
		t.Fatalf("what the orphan said is not kept: %v in %+v", texts, msgs[2].Content)
	}
}

// A result with no call anywhere in the request keeps what it said as text,
// beside what the user said, in the one message they build.
func TestMessagesOrphanToolResultWithoutACallBecomesText(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{result("gone", "late"), {Kind: Text, Text: "go on"}}},
	}}
	trace, msgs := messagesTrace(t, r)
	want := "user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	last := msgs[len(msgs)-1].Content
	if last[0].Type != "text" || last[0].Text != "late" {
		t.Fatalf("the orphan is not text: %+v", last[0])
	}
	if last[1].Type != "text" || last[1].Text != "go on" {
		t.Fatalf("what the user said moved: %+v", last[1])
	}
}

// A conversation that ends on its calls — the client asks the turn to go on
// — ends on their synthetic results, as a Chat request ends on theirs.
func TestMessagesUnansweredCallAtTheEnd(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run it"}}},
		{Role: "assistant", Parts: []Part{call("A")}},
	}}
	trace, msgs := messagesTrace(t, r)
	want := "user assistant(A) user[A]"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !anthropicValid(msgs) {
		t.Fatal("sequence fails Anthropic's tool exchange validation")
	}
}

// A well-formed exchange is built exactly as before: parallel results out
// of order keep their order, a result keeps its place before the text
// beside it, and every id goes where it did.
func TestMessagesCleanExchangeUntouched(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run them"}}},
		{Role: "assistant", Parts: []Part{call("A"), call("B")}},
		{Role: "user", Parts: []Part{result("B", "second"), result("A", "first"), {Kind: Text, Text: "done"}}},
	}}
	trace, msgs := messagesTrace(t, r)
	want := "user assistant(A,B) user[B,A]"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !anthropicValid(msgs) {
		t.Fatal("sequence fails Anthropic's tool exchange validation")
	}
	third := msgs[2].Content
	if third[0].Type != "tool_result" || third[0].ToolUseID != "B" || third[1].ToolUseID != "A" || third[2].Text != "done" {
		t.Fatalf("results or text moved: %+v", third)
	}
}

// Another vendor's call id ("Bash:0#…", 6c099edd) still goes out through
// anthropicOutID when the exchange is mended, and the call and its
// synthetic result keep matching.
func TestMessagesMendedExchangeKeepsForeignIDsEncoded(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run it"}}},
		{Role: "assistant", Parts: []Part{call("Bash:0#a65b6a5e")}},
		{Role: "user", Parts: []Part{result("gone", "late")}},
	}}
	_, msgs := messagesTrace(t, r)
	id := anthropicOutID("Bash:0#a65b6a5e")
	if msgs[1].Content[0].ID != id {
		t.Fatalf("the call's id is not encoded as the Messages path does: %+v", msgs[1].Content[0])
	}
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "tool_result" && c.ToolUseID == "gone" {
				t.Fatalf("the orphan result survived as a result: %+v", c)
			}
			if c.Type == "tool_result" && c.ToolUseID != id {
				t.Fatalf("the synthetic result names another id: %+v", c)
			}
		}
	}
	if !anthropicValid(msgs) {
		t.Fatal("sequence fails Anthropic's tool exchange validation")
	}
}

// An interrupted turn and a late result, sent by an agent that speaks Chat
// to a provider that serves the model over Messages alone: the upstream is
// asked a mended exchange and answers, where the broken one was refused 400
// and the turn failed.
func TestMessagesUpstreamAnswersABrokenToolExchange(t *testing.T) {
	f := &fake{t: t, refuse: validateExchange, reply: sse(
		`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"x","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`data: {"type":"message_stop"}`,
	)}
	setup(t, provider.Anthropic, f)
	body := `{"model":"fake/m1","stream":true,"messages":[` +
		`{"role":"user","content":"run it"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"run","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_gone","content":"late"},` +
		`{"role":"user","content":"go on"}]}`
	if code, out := post(t, "/v1/chat/completions", body); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	if f.path != "/v1/messages" {
		t.Fatalf("not translated to Messages: %s", f.path)
	}
	if code, msg := validateExchange(f.got); code != 0 {
		t.Fatalf("the upstream was asked a broken exchange: %s", msg)
	}
	if !strings.Contains(string(f.got), `"tool_use_id":"call_1"`) {
		t.Fatalf("no result for the interrupted call reached the upstream: %s", f.got)
	}
	if strings.Contains(string(f.got), `"tool_use_id":"call_gone"`) {
		t.Fatalf("the orphan result reached the upstream as one: %s", f.got)
	}
}

// A call answered twice keeps its first answer, and only that one goes
// out: Anthropic takes one tool_result per tool_use.
func TestMessagesSecondAnswerDropped(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run it"}}},
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "user", Parts: []Part{result("A", "first"), result("A", "again")}},
	}}
	trace, msgs := messagesTrace(t, r)
	if want := "user assistant(A) user[A]"; trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if got := string(msgs[2].Content[0].Content); got != `"first"` {
		t.Fatalf("the first answer didn't stand: %s", got)
	}
}
