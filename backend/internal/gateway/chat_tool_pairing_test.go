package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// chatTrace renders the message sequence of a Chat request built for an
// upstream: roles with tool call ids, e.g.
// "user assistant(A,B) tool(A) tool(B) user".
func chatTrace(t *testing.T, r *Request) (string, []map[string]any) {
	t.Helper()
	body := buildChat(r, "kimi-k3", "api.moonshot.cn", false)
	var q struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		t.Fatalf("built request does not parse: %v", err)
	}
	var b strings.Builder
	for _, m := range q.Messages {
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
	return strings.TrimSpace(b.String()), q.Messages
}

// kimiValid reports whether a built message sequence passes what Kimi
// validates: every tool message answers a call of the assistant message
// it follows, with only tool messages between.
func kimiValid(msgs []map[string]any) bool {
	var pending map[string]bool
	for _, m := range msgs {
		switch m["role"] {
		case "assistant":
			pending = nil
			if calls, ok := m["tool_calls"].([]any); ok {
				pending = map[string]bool{}
				for _, c := range calls {
					id, _ := c.(map[string]any)["id"].(string)
					pending[id] = true
				}
			}
		case "tool":
			id, _ := m["tool_call_id"].(string)
			if !pending[id] {
				return false
			}
		default:
			pending = nil
		}
	}
	return true
}

// idsUsedOnce reports whether every tool_call_id takes part in one
// exchange only: one assistant call, one tool answer at most. A strict
// upstream can turn away an id called or answered twice.
func idsUsedOnce(msgs []map[string]any) bool {
	calls, answers := map[string]int{}, map[string]int{}
	for _, m := range msgs {
		if cs, ok := m["tool_calls"].([]any); ok {
			for _, c := range cs {
				id, _ := c.(map[string]any)["id"].(string)
				calls[id]++
			}
		}
		if m["role"] == "tool" {
			id, _ := m["tool_call_id"].(string)
			answers[id]++
		}
	}
	for id, n := range calls {
		if id == "" || n > 1 {
			return false
		}
	}
	for id, n := range answers {
		if id == "" || n > 1 {
			return false
		}
	}
	return true
}

func call(id string) Part {
	return Part{Kind: ToolCall, ID: id, Name: "run", Args: json.RawMessage(`{}`)}
}
func result(id, out string) Part {
	return Part{Kind: ToolResult, CallID: id, Text: out}
}

// Claude Code makes parallel calls in one turn and the results come back
// out of order, beside the turn's own text (an attachment's reminder):
// the tool messages must lead, in the calls' order, the text after.
func TestChatToolResultsLeadInCallOrder(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "run them"}}},
		{Role: "assistant", Parts: []Part{call("A"), call("B"), call("C"), call("D")}},
		{Role: "user", Parts: []Part{
			{Kind: Text, Text: "note from an attachment"},
			result("C", "3"), result("A", "1"), result("D", "4"), result("B", "2"),
		}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "user assistant(A,B,C,D) tool(A) tool(B) tool(C) tool(D) user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !kimiValid(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// Results split over consecutive user messages (Claude Code logs one per
// message) still gather behind their assistant message, in call order.
func TestChatToolResultsGatherAcrossUserMessages(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "assistant", Parts: []Part{call("A"), call("B")}},
		{Role: "user", Parts: []Part{result("B", "2")}},
		{Role: "user", Parts: []Part{result("A", "1")}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "assistant(A,B) tool(A) tool(B)"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !kimiValid(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// A result whose call is gone (compaction cut it, or a client sends
// results alone) goes to the model as a user message, the way the
// Responses path's orphanedToolOutputs delivers it: no made-up call, no
// id used twice.
func TestChatOrphanToolResultBecomesUserMessage(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{result("X", "kept")}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "go on"}}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "user user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if c, _ := msgs[0]["content"].(string); c != "kept" {
		t.Fatalf("orphan result content = %q", c)
	}
	if !kimiValid(msgs) || !idsUsedOnce(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// A late result — its call's exchange is closed already (assistant(a),
// user text, user(tool_result a); Codex sends function_call, message,
// function_call_output) — must not reuse the id in a second exchange: it
// joins the user message its exchange closed with.
func TestChatLateToolResultJoinsUserMessage(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "note"}}},
		{Role: "user", Parts: []Part{result("A", "real result")}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "assistant(A) tool(A) user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if c, _ := msgs[1]["content"].(string); !strings.Contains(c, "unavailable") {
		t.Fatalf("closed exchange's synthetic result = %q", c)
	}
	c, _ := msgs[2]["content"].(string)
	if !strings.Contains(c, "note") || !strings.Contains(c, "real result") {
		t.Fatalf("user message = %q", c)
	}
	if !kimiValid(msgs) || !idsUsedOnce(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// Two results for the same call: the first answer stands, the second is
// dropped — a strict upstream refuses an id answered twice.
func TestChatDuplicateToolResultsKeepFirst(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "user", Parts: []Part{result("A", "1"), result("A", "1 again")}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "assistant(A) tool(A)"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if c, _ := msgs[1]["content"].(string); c != "1" {
		t.Fatalf("answer = %q, want the first", c)
	}
	if !kimiValid(msgs) || !idsUsedOnce(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// A result with an empty tool_call_id is an orphan too: it becomes a
// user message, never a call with id "".
func TestChatEmptyToolCallIDBecomesUserMessage(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{result("", "no call")}},
	}}
	trace, msgs := chatTrace(t, r)
	if trace != "user" {
		t.Fatalf("trace = %q, want %q", trace, "user")
	}
	if c, _ := msgs[0]["content"].(string); c != "no call" {
		t.Fatalf("content = %q", c)
	}
	if !idsUsedOnce(msgs) {
		t.Fatal("an empty tool_call_id leaked into the exchange")
	}
}

// An interrupted turn leaves a call unanswered: it gets a synthetic error
// result so the next turn can start.
func TestChatUnansweredCallGetsSyntheticResult(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "继续"}}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "assistant(A) tool(A) user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if c, _ := msgs[1]["content"].(string); !strings.Contains(c, "unavailable") {
		t.Fatalf("synthetic result content = %q", c)
	}
	if !kimiValid(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// A thinking-only assistant turn (what an aborted stream leaves behind)
// inside a pending exchange is dropped before it can sit between the
// calls and their answers — and the exchange stays open, so the real
// result still answers its call instead of a synthetic "interrupted".
func TestChatThinkingOnlyAssistantDroppedInsideExchange(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "assistant", Parts: []Part{call("A")}},
		{Role: "assistant", Parts: []Part{{Kind: Thinking, Text: "hmm", Signature: "sig"}}},
		{Role: "user", Parts: []Part{result("A", "1")}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "assistant(A) tool(A)"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if c, _ := msgs[1]["content"].(string); c != "1" {
		t.Fatalf("answer = %q, want the real result", c)
	}
	if !kimiValid(msgs) || !idsUsedOnce(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// Outside an exchange a thinking-only turn, one the user cut off before
// it said anything, is left out too: DeepSeek and Kimi turn the request
// away over an assistant message with neither content nor tool_calls
// (#2599). The two user messages it stood between join into one, as some
// models' chat templates turn away two in a row.
func TestChatThinkingOnlyAssistantLeftOutOutsideExchange(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}},
		{Role: "assistant", Parts: []Part{{Kind: Thinking, Text: "hmm", Signature: "sig"}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "继续"}}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if c, _ := msgs[0]["content"].(string); c != "hi\n\n继续" {
		t.Fatalf("joined content = %q", c)
	}
}

// The shape a real Claude Code session replays to (97 clean exchanges in
// the session this was taken from), as a fixed fixture: parallel calls
// answered out of order across user messages, an interrupted turn whose
// late result follows its own text, and a clean turn — all in one
// request, valid and byte-stable for the clean parts.
func TestChatSessionShapeStaysValid(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "look into it"}}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "on it"}}},
		{Role: "assistant", Parts: []Part{call("A"), call("B")}},
		{Role: "user", Parts: []Part{result("B", "2")}},
		{Role: "user", Parts: []Part{result("A", "1")}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "one more"}}},
		{Role: "assistant", Parts: []Part{call("C")}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "interrupted"}}},
		{Role: "user", Parts: []Part{result("C", "late")}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "done"}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "thanks"}}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "user assistant assistant(A,B) tool(A) tool(B) assistant assistant(C) tool(C) user assistant user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !kimiValid(msgs) || !idsUsedOnce(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}

// A clean exchange goes through untouched.
func TestChatCleanExchangeUntouched(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "hello"}}},
		{Role: "assistant", Parts: []Part{call("A"), call("B")}},
		{Role: "user", Parts: []Part{result("A", "1"), result("B", "2")}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "thanks"}}},
	}}
	trace, msgs := chatTrace(t, r)
	want := "user assistant assistant(A,B) tool(A) tool(B) user"
	if trace != want {
		t.Fatalf("trace = %q, want %q", trace, want)
	}
	if !kimiValid(msgs) {
		t.Fatal("sequence fails Kimi's tool exchange validation")
	}
}
