package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// dsRefuseEmptyAssistant answers as DeepSeek's and Kimi's Chat APIs do,
// in the bytes of #2599, for a turn the agent cut off before it said
// anything: an assistant message with neither content nor tool_calls is
// invalid however much reasoning_content it carries, and the whole request
// is turned away. DeepSeek's rule for a tool turn without its reasoning
// (#1462, #1822) holds as well.
func dsRefuseEmptyAssistant(body []byte) (int, string) {
	var q struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &q)
	for _, m := range q.Messages {
		if string(m["role"]) != `"assistant"` {
			continue
		}
		var calls []json.RawMessage
		if json.Unmarshal(m["tool_calls"], &calls) == nil && len(calls) > 0 {
			continue
		}
		var s string
		var parts []struct {
			Text string `json:"text"`
		}
		empty := m["content"] == nil || string(m["content"]) == "null"
		if json.Unmarshal(m["content"], &s) == nil {
			empty = strings.TrimSpace(s) == ""
		} else if json.Unmarshal(m["content"], &parts) == nil {
			empty = true
			for _, p := range parts {
				empty = empty && strings.TrimSpace(p.Text) == ""
			}
		}
		if empty {
			return 400, `{"error":{"message":"Invalid assistant message: content or tool_calls must be set","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`
		}
	}
	return deepseekRefuseToolTurns(body)
}

const emptyOKStream = `data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","content":"Yes."},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}` + "\n\ndata: [DONE]\n\n"

// dsUpstream is a DeepSeek-strict Chat upstream behind provider "ds".
func dsUpstream(t *testing.T) *fake {
	f := &fake{t: t, reply: emptyOKStream, refuse: dsRefuseEmptyAssistant}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "ds", Models: []string{"deepseek-v4.1-flash", "glm-5.2"}})
	return f
}

// sentRoles is the roles of the messages an upstream was sent, with the
// last one's content.
func sentRoles(t *testing.T, got []byte) (string, string) {
	t.Helper()
	var roles []string
	var last string
	for _, raw := range sentMessages(t, got) {
		var m struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		json.Unmarshal(raw, &m)
		roles = append(roles, m.Role)
		last = string(m.Content)
	}
	return strings.Join(roles, " "), last
}

// A reply Claude Code's user cut off before it said anything — only its
// thinking, only an empty text, nothing at all — stays in its history and
// is sent again with every later turn. Translated to a Chat upstream it
// went out as an assistant message with "content": "", and DeepSeek and
// Kimi turned every request of the conversation away. It is left out, and
// the two user messages around it join into one. After a tool result the
// user's message follows the tool message, as when they write there.
func TestChatEmptyAssistantLeftOutFromAnthropic(t *testing.T) {
	f := dsUpstream(t)
	for name, turn := range map[string]string{
		"thinking":        `[{"type":"thinking","thinking":"mid-thought","signature":"sig"}]`,
		"redacted":        `[{"type":"redacted_thinking","data":"gAAAAABpZm9vYmFy"}]`,
		"empty text":      `[{"type":"text","text":""}]`,
		"thinking + text": `[{"type":"thinking","thinking":"mid-thought","signature":"sig"},{"type":"text","text":""}]`,
		"no blocks":       `[]`,
		"empty string":    `""`,
	} {
		t.Run(name, func(t *testing.T) {
			f.calls = 0
			code, body := postTo(t, New(), "/v1/messages", `{"model":"ds/deepseek-v4.1-flash","max_tokens":32,"messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":`+turn+`},
				{"role":"user","content":"continue"}]}`)
			if code != 200 || f.calls != 1 {
				t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
			}
			if shape, last := sentRoles(t, f.got); shape != "user" || last != `"hi\n\ncontinue"` {
				t.Fatalf("sent %s", f.got)
			}
		})
	}
	t.Run("after a tool result", func(t *testing.T) {
		f.calls = 0
		code, body := postTo(t, New(), "/v1/messages", `{"model":"ds/deepseek-v4.1-flash","max_tokens":32,
			"tools":[{"name":"get_time","input_schema":{"type":"object","properties":{}}}],"messages":[
			{"role":"user","content":"what time is it?"},
			{"role":"assistant","content":[{"type":"thinking","thinking":"Ask the clock.","signature":"sig"},{"type":"tool_use","id":"toolu_1","name":"get_time","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"12:00"}]},
			{"role":"assistant","content":[{"type":"thinking","thinking":"It is noon, so","signature":"sig"}]},
			{"role":"user","content":"continue"}]}`)
		if code != 200 || f.calls != 1 {
			t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
		}
		if shape, last := sentRoles(t, f.got); shape != "user assistant tool user" || last != `"continue"` {
			t.Fatalf("sent %s", f.got)
		}
	})
}

// The same from Codex: a cut-off reply is an isolated reasoning item, or
// an assistant message with an empty output_text.
func TestChatEmptyAssistantLeftOutFromResponses(t *testing.T) {
	f := dsUpstream(t)
	for name, turn := range map[string]string{
		"reasoning":   `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"mid-thought"}]}`,
		"empty text":  `{"type":"message","role":"assistant","content":[{"type":"output_text","text":""}]}`,
		"no contents": `{"type":"message","role":"assistant","content":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f.calls = 0
			code, body := postTo(t, New(), "/v1/responses", `{"model":"ds/deepseek-v4.1-flash","stream":false,"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
				`+turn+`,
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
			if code != 200 || f.calls != 1 {
				t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
			}
			if shape, last := sentRoles(t, f.got); shape != "user" || last != `"hi\n\ncontinue"` {
				t.Fatalf("sent %s", f.got)
			}
		})
	}
}

// And from Gemini CLI: a model turn with only its thought.
func TestChatEmptyAssistantLeftOutFromGemini(t *testing.T) {
	f := dsUpstream(t)
	code, body := postTo(t, New(), "/v1beta/models/ds/deepseek-v4.1-flash:streamGenerateContent?alt=sse", `{"contents":[
		{"role":"user","parts":[{"text":"hi"}]},
		{"role":"model","parts":[{"text":"mid-thought","thought":true}]},
		{"role":"user","parts":[{"text":"continue"}]}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
	}
	if shape, last := sentRoles(t, f.got); shape != "user" || last != `"hi\n\ncontinue"` {
		t.Fatalf("sent %s", f.got)
	}
}

// A Chat agent (OpenCode, Kimi Code) keeps the turn it cut off as it
// came, {"role":"assistant","content":""} or null with reasoning_content
// alone, and its request is relayed as it sent it: the turn is left out
// there too, whichever model, and the user messages around it join.
func TestChatRelayLeavesOutEmptyAssistant(t *testing.T) {
	f := dsUpstream(t)
	srv := New()
	for _, c := range []struct{ name, model, turns, shape, last string }{
		{"empty", "ds/deepseek-v4.1-flash", `{"role":"user","content":"hi"},{"role":"assistant","content":"","reasoning_content":"halfway"},{"role":"user","content":"continue"}`,
			"user", `"hi\n\ncontinue"`},
		{"null", "ds/glm-5.2", `{"role":"user","content":"hi"},{"role":"assistant","content":null,"reasoning_content":"halfway"},{"role":"user","content":"continue"}`,
			"user", `"hi\n\ncontinue"`},
		{"no content", "ds/glm-5.2", `{"role":"user","content":"hi"},{"role":"assistant"},{"role":"user","content":"continue"}`,
			"user", `"hi\n\ncontinue"`},
		{"blank parts", "ds/glm-5.2", `{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"text","text":""}]},{"role":"user","content":[{"type":"text","text":"continue"}]}`,
			"user", `"hi\n\ncontinue"`},
		{"after a tool result", "ds/deepseek-v4.1-flash", `{"role":"user","content":"what time is it?"},
			{"role":"assistant","content":null,"reasoning_content":"Ask.","tool_calls":[` + timeCall + `]},
			{"role":"tool","tool_call_id":"call_1","content":"12:00"},
			{"role":"assistant","content":"","reasoning_content":"It is noon, so"},
			{"role":"user","content":"continue"}`,
			"user assistant tool user", `"continue"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.calls = 0
			code, body := postTo(t, srv, "/v1/chat/completions", `{"model":"`+c.model+`","stream":true,"messages":[`+c.turns+`]}`)
			if code != 200 || f.calls != 1 {
				t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
			}
			if shape, last := sentRoles(t, f.got); shape != c.shape || last != c.last {
				t.Fatalf("sent %s", f.got)
			}
		})
	}
}

// What says something stays as the client sent it: a body without a
// silent turn byte for byte, and in one with, a legacy function_call, a
// prefill to go on from, a refusal, and a turn of text.
func TestChatRelaySilentAssistantsOnly(t *testing.T) {
	clean := []byte(`{"model":"m","messages":[{"role":"user","content":"a <b> & c"},{"role":"assistant","content":"x","n":1.50},{"role":"user","content":"go"}]}`)
	if got := silentAssistantsOut(clean); !bytes.Equal(got, clean) {
		t.Fatalf("a clean body was rewritten: %s", got)
	}
	body := []byte(`{"model":"m","temperature":0.70,"messages":[
		{"role":"user","content":"a <b> & c"},
		{"role":"assistant","content":null,"function_call":{"name":"f","arguments":"{}"}},
		{"role":"function","name":"f","content":"1"},
		{"role":"assistant","content":"","refusal":"I can't."},
		{"role":"user","content":"then"},
		{"role":"assistant","content":"","reasoning_content":"cut"},
		{"role":"user","content":"go"},
		{"role":"assistant","content":"","prefix":true}]}`)
	got := silentAssistantsOut(body)
	var q struct {
		Temperature json.RawMessage   `json:"temperature"`
		Messages    []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(got, &q); err != nil {
		t.Fatal(err)
	}
	if string(q.Temperature) != "0.70" || !bytes.Contains(got, []byte(`"a <b> & c"`)) {
		t.Errorf("the rest of the body changed: %s", got)
	}
	want := []string{
		`{"role":"user","content":"a <b> & c"}`,
		`{"role":"assistant","content":null,"function_call":{"name":"f","arguments":"{}"}}`,
		`{"role":"function","name":"f","content":"1"}`,
		`{"role":"assistant","content":"","refusal":"I can't."}`,
		`{"role":"user","content":"then\n\ngo"}`,
		`{"role":"assistant","content":"","prefix":true}`,
	}
	if len(q.Messages) != len(want) {
		t.Fatalf("sent %s", got)
	}
	for i, w := range want {
		sameJSON(t, q.Messages[i], w)
	}
}

// silentAssistantsOut runs on every attempt of a relayed Chat request, and
// a long conversation with nothing to leave out costs it a look at the
// assistant turns, not a decode of every message: it decoded all of a
// 1.5 MB conversation into maps on each attempt, ~4 MB allocated a request
// (yg6888 on X). A silent turn at the very end, or written with escapes,
// is still found.
func TestSilentAssistantsOutDecodesOnlyAssistants(t *testing.T) {
	var turns []string
	for i := range 200 {
		turns = append(turns, `{"role":"user","content":"`+strings.Repeat("read this file ", 300)+`"}`,
			`{"role":"assistant","content":"ok `+strings.Repeat("x", i)+`"}`)
	}
	body := []byte(`{"model":"m","messages":[` + strings.Join(turns, ",") + `]}`)
	ctx, done := context.WithCancel(context.Background())
	defer done()
	holdBody(ctx, body) // as readRequestBody holds a request's
	if got := silentAssistantsOut(body); !bytes.Equal(got, body) {
		t.Fatal("a clean body was rewritten")
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const n = 5
	for range n {
		silentAssistantsOut(body)
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / n; per > uint64(len(body))/8 {
		t.Fatalf("looking at a %d-byte clean body allocated %d bytes", len(body), per)
	}

	for _, last := range []string{
		`{"role":"assistant","content":"","reasoning_content":"cut"}`,
		`{"role":"assistant","content":null}`,
		`{"role":"%u0061ssistant","content":null}`,
		`{"role":"assistant","content":"\n　\t "}`,
		`{"role":"assistant","content":"\n%u3000\t "}`,
		`{"content":[{"type":"text","text":" "}],"role":"assistant"}`,
	} {
		b := []byte(`{"model":"m","messages":[` + strings.Join(turns, ",") + `,` + strings.ReplaceAll(last, "%u", "\x5cu") + `,{"role":"user","content":"go"}]}`)
		var q struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(silentAssistantsOut(b), &q); err != nil || len(q.Messages) != len(turns)+1 {
			t.Fatalf("%s: %d messages sent, want %d (%v)", last, len(q.Messages), len(turns)+1, err)
		}
	}
}

// A Resume's prefill, uni-switch's own ask to go on from a reply cut short,
// stays however little of the reply there was: without it the upstream
// would answer from the start instead of going on.
func TestChatResumeEmptyPrefillKept(t *testing.T) {
	r := &Request{Resume: true, Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "go on"}}},
		{Role: "assistant", Parts: []Part{{Kind: Thinking, Text: "half"}}},
	}}
	var q struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(buildChat(r, "deepseek-chat", "api.deepseek.com", false), &q); err != nil {
		t.Fatal(err)
	}
	last := q.Messages[len(q.Messages)-1]
	if last["role"] != "assistant" || last["prefix"] != true || last["reasoning_content"] != "half" {
		t.Fatalf("prefill = %v, want it kept with its prefix and reasoning", last)
	}
}
