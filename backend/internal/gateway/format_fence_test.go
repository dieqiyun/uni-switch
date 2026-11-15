package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// fencedChat is a Chat upstream that answers every request with the JSON
// wrapped in a Markdown code fence, streamed in pieces that split the fence.
type fencedChat struct{ answer string }

func (f fencedChat) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(s string) string {
		b, _ := json.Marshal(s)
		return `data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":` + string(b) + `}}]}`
	}
	a := f.answer
	io.WriteString(w, sse(chunk(a[:2]), chunk(a[2:9]), chunk(a[9:len(a)-2]), chunk(a[len(a)-2:]),
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}`,
		`data: [DONE]`))
}

// responsesText is the answer's text in a Responses reply, streamed or whole.
func responsesText(t *testing.T, out string, stream bool) string {
	t.Helper()
	if !stream {
		var res struct {
			Output []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		var b strings.Builder
		for _, o := range res.Output {
			for _, c := range o.Content {
				b.WriteString(c.Text)
			}
		}
		return b.String()
	}
	var b strings.Builder
	for _, ev := range events(out) {
		if ev["type"] == "response.output_text.delta" {
			d, _ := ev["delta"].(string)
			b.WriteString(d)
		}
	}
	return b.String()
}

// A structured-output client gets the JSON it asked for when the model
// answers it in a code fence: Codex's memory extraction failed with
// "expected value at line 1 column 1" on every such answer.
func TestStructuredOutputFencedAnswerUnfenced(t *testing.T) {
	const answer = "```json\n" + calendarAnswer + "\n```"
	for _, c := range []struct {
		name   string
		format string
		want   string
	}{
		{"json_schema", `,"text":{"format":{"type":"json_schema","strict":true,"name":"CalendarEvent","schema":` + calendarSchema + `}}`, calendarAnswer},
		{"json_object", `,"text":{"format":{"type":"json_object"}}`, calendarAnswer},
		// a client that asked for plain text gets the model's words as they came
		{"no format", ``, answer},
	} {
		for _, stream := range []bool{false, true} {
			name := c.name + map[bool]string{false: " whole", true: " streamed"}[stream]
			t.Run(name, func(t *testing.T) {
				fresh(t)
				up := httptest.NewServer(fencedChat{answer: answer})
				t.Cleanup(up.Close)
				if err := provider.Save(provider.Provider{ID: "chat", Name: "Chat", Key: "k", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
					t.Fatal(err)
				}
				s := "false"
				if stream {
					s = "true"
				}
				code, out := post(t, "/v1/responses", `{"model":"chat/m1","input":"Alice and Bob are going to a science fair on Friday.","stream":`+s+c.format+`}`)
				if code != 200 {
					t.Fatalf("status %d: %s", code, out)
				}
				if got := responsesText(t, out, stream); got != c.want {
					t.Errorf("text = %q, want %q", got, c.want)
				}
			})
		}
	}
}

func TestUnfence(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"  ```\n[1,2]\n```  \n", `[1,2]`},
		{"```JSON\r\n{\"a\":1}\r\n```", `{"a":1}`},
		{"```json\n{\"raw_memory\":\"Keep ``` code samples\",\"rollout_summary\":\"test\"}\n```", "{\"raw_memory\":\"Keep ``` code samples\",\"rollout_summary\":\"test\"}"},
		{"```json\n{\"a\":}\n```", "```json\n{\"a\":}\n```"},
		{"```json\n{\"a\":1", "```json\n{\"a\":1"},
		{"```python\n{}\n```", "```python\n{}\n```"},
		{"```json\n{} {}\n```", "```json\n{} {}\n```"},
		// not a fence around the whole answer: left as it came
		{`{"a":"` + "```" + `"}`, `{"a":"` + "```" + `"}`},
		{"Here it is:\n```json\n{}\n```", "Here it is:\n```json\n{}\n```"},
		{"```json {\"a\":1}```", "```json {\"a\":1}```"},
		{"```json\n{}\n```\nand\n```json\n{}\n```", "```json\n{}\n```\nand\n```json\n{}\n```"},
		{"```\n```", "```\n```"},
	} {
		if got, _ := unfence(c.in); got != c.want {
			t.Errorf("unfence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Moving the schema into instructions must not lose the client's JSON
// requirement, and buffering its text must preserve reasoning, usage and IDs.
func TestStructuredOutputFenceAfterSystemFallback(t *testing.T) {
	const memory = "{\"raw_memory\":\"Keep ``` examples\",\"rollout_summary\":\"test\"}"
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole", true: "streamed"}[stream], func(t *testing.T) {
			req := (&Request{Model: "m", Stream: stream, Format: &Format{Type: "json_object"}}).inSystem()
			input := []Event{
				{Kind: KStart, MsgID: "resp_fence"},
				{Kind: KThink, Text: "reasoning"},
				{Kind: KText, Text: "``"},
				{Kind: KText, Text: "`json\n" + memory},
				{Kind: KUsage, Usage: Usage{Input: 5, Output: 8}},
				{Kind: KText, Text: "\n```"},
				{Kind: KStop, Stop: "stop"},
			}
			ch := make(chan Event, len(input))
			for _, ev := range input {
				ch <- ev
			}
			close(ch)
			rec := httptest.NewRecorder()
			var u Usage
			code, failed := relayStatus(rec, provider.Responses, "fixture", req, ch, &u, func() {})
			if code != 200 || failed != "" {
				t.Fatalf("status %d: %s", code, failed)
			}
			out := rec.Body.String()
			if got := responsesText(t, out, stream); got != memory {
				t.Fatalf("text = %q, want %q", got, memory)
			}
			res, err := compactReply([]byte(out))
			if err != nil || messageText(res) != memory || res.Status != "completed" {
				t.Fatalf("final reply: %+v, error %v", res, err)
			}
			if !strings.Contains(out, "reasoning") || u.ResponseID != "resp_fence" || u.Input != 5 || u.Output != 8 {
				t.Fatalf("metadata lost: usage %+v, reply %s", u, out)
			}
		})
	}
}

func TestStructuredOutputFencePreservesIncompleteReplies(t *testing.T) {
	const partial = "```json\n{\"raw_memory\":"
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "length", true: "error"}[failed], func(t *testing.T) {
			rec := httptest.NewRecorder()
			enc := encoder(provider.Responses, newSSEWriter(rec), &Request{Model: "m", Format: &Format{Type: "json_object"}})
			enc.event(Event{Kind: KText, Text: partial})
			if failed {
				enc.event(Event{Kind: KError, Text: "upstream interrupted", Code: "server_error"})
			} else {
				enc.event(Event{Kind: KStop, Stop: "length"})
				enc.finish()
			}
			out := rec.Body.String()
			if got := responsesText(t, out, true); got != partial {
				t.Fatalf("partial text = %q, want %q", got, partial)
			}
			want := "response.incomplete"
			if failed {
				want = "response.failed"
			}
			if !strings.Contains(out, want) || strings.Contains(out, "response.completed") {
				t.Fatalf("wrong completion: %s", out)
			}
		})
	}
}

func TestStructuredOutputUnfencedTextStreamsImmediately(t *testing.T) {
	rec := httptest.NewRecorder()
	enc := encoder(provider.Responses, newSSEWriter(rec), &Request{Model: "m", Format: &Format{Type: "json_object"}})
	enc.event(Event{Kind: KText, Text: `{"raw_memory":`})
	if got := responsesText(t, rec.Body.String(), true); got != `{"raw_memory":` {
		t.Fatalf("initial delta buffered: %q", got)
	}
}
