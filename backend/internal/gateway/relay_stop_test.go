package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// Anthropic-shaped relays in front of other vendors' models (Grok's, as
// 蓝猫 on Discord had it) end their streams in ways Anthropic's own API
// doesn't: a reply of tool calls whose stop_reason says end_turn, or
// OpenAI's "tool_calls"; a tool_use block whose content_block_start
// carries the whole input and no input_json_delta after it; a stream that
// just stops, with no stop_reason and no message_stop. An agent told such
// a reply ended its turn ends it, a few words in, as if done.

// relayAnthropic serves a translated request's model from an Anthropic
// upstream answering with events, as they are, and returns the Chat
// stream uni-switch made of it.
func relayAnthropic(t *testing.T, events ...string) string {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			io.WriteString(w, "data: "+ev+"\n\n")
		}
	}))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"grok-4"}, Anthropic: up.URL}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	code, body := post(t, "/v1/chat/completions", `{"model":"up/grok-4","stream":true,"messages":[{"role":"user","content":"list files"}],`+
		`"tools":[{"type":"function","function":{"name":"Bash","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	return body
}

// chatFinish is a Chat stream's finish_reason, chatArgs the arguments of
// its tool calls, run together.
func chatFinish(body string) string {
	var out string
	for _, ev := range events(body) {
		for _, c := range choices(ev) {
			if s, _ := c["finish_reason"].(string); s != "" {
				out = s
			}
		}
	}
	return out
}

func chatArgs(body string) string {
	var sb strings.Builder
	for _, ev := range events(body) {
		for _, c := range choices(ev) {
			d, _ := c["delta"].(map[string]any)
			calls, _ := d["tool_calls"].([]any)
			for _, x := range calls {
				call, _ := x.(map[string]any)
				fn, _ := call["function"].(map[string]any)
				s, _ := fn["arguments"].(string)
				sb.WriteString(s)
			}
		}
	}
	return sb.String()
}

const (
	relayToolStart = `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}}`
	relayToolArgs  = `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}`
	relayToolStop  = `{"type":"content_block_stop","index":1}`
	relayEnd       = `{"type":"message_stop"}`
)

func relayStop(reason string) string {
	return `{"type":"message_delta","delta":{"stop_reason":"` + reason + `"},"usage":{"output_tokens":9}}`
}

// A reply of tool calls is the agent's to run, whatever stop_reason the
// relay gave it.
func TestRelayToolCallsStopForTools(t *testing.T) {
	for _, reason := range []string{"end_turn", "tool_calls", "stop"} {
		t.Run(reason, func(t *testing.T) {
			body := relayAnthropic(t, anthStart("grok-4", 5), anthText("Let me look."), relayToolStart, relayToolArgs, relayToolStop, relayStop(reason), relayEnd)
			if got := chatFinish(body); got != "tool_calls" {
				t.Errorf("finish_reason %q, want tool_calls: %s", got, body)
			}
			if got := chatArgs(body); got != `{"command":"ls"}` {
				t.Errorf("arguments %q", got)
			}
		})
	}
}

// A tool_use block that starts with its whole input, no delta after it,
// keeps that input.
func TestRelayToolInputInStart(t *testing.T) {
	start := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}}`
	body := relayAnthropic(t, anthStart("grok-4", 5), start, `{"type":"content_block_stop","index":0}`, relayStop("tool_use"), relayEnd)
	if got := chatArgs(body); got != `{"command":"ls"}` {
		t.Errorf("arguments %q, want the start's input: %s", got, body)
	}
	if got := chatFinish(body); got != "tool_calls" {
		t.Errorf("finish_reason %q", got)
	}
}

// A stream that stops with no stop_reason and no message_stop is a reply
// cut short, and the client is told so rather than handed a finished one.
func TestRelayStreamEndedShort(t *testing.T) {
	body := relayAnthropic(t, anthStart("grok-4", 5), anthText("Let me look at the"))
	if chatFinish(body) == "stop" {
		t.Errorf("a cut reply finished as whole: %s", body)
	}
	if !strings.Contains(body, "ended before it was complete") {
		t.Errorf("no error for the cut reply: %s", body)
	}
	// a stream with a stop_reason and no message_stop said it was done
	body = relayAnthropic(t, anthStart("grok-4", 5), anthText("Done."), relayStop("end_turn"))
	if got := chatFinish(body); got != "stop" || strings.Contains(body, "ended before") {
		t.Errorf("finish_reason %q: %s", got, body)
	}
}

// relayResponses serves a translated request's model from a Responses
// upstream answering with events, as they are, and returns the Chat
// stream uni-switch made of it.
func relayResponses(t *testing.T, events ...string) string {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			io.WriteString(w, "data: "+ev+"\n\n")
		}
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"gpt-6.1"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	code, body := post(t, "/v1/chat/completions", `{"model":"up/gpt-6.1","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	return body
}

// A Responses stream that stops with no response.completed is a reply cut
// short, as an Anthropic one with no message_stop is: the client is told
// so rather than handed a finished one (the passthrough's word for it,
// which the translated path gave only Anthropic's streams).
func TestResponsesUpstreamEndedShort(t *testing.T) {
	body := relayResponses(t,
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"Let me look at the"}`)
	if chatFinish(body) == "stop" && !strings.Contains(body, "ended before it was complete") {
		t.Errorf("a cut reply finished as whole: %s", body)
	}
	if !strings.Contains(body, "ended before it was complete") {
		t.Errorf("no error for the cut reply: %s", body)
	}
	// a stream whose response.completed came said it was done
	body = relayResponses(t,
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Done."}]}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":1}}}`)
	if got := chatFinish(body); got != "stop" || strings.Contains(body, "ended before") {
		t.Errorf("finish_reason %q: %s", got, body)
	}
}

// relayUpstreamWhole serves a translated request's model from an upstream
// answering with events and asks for the reply not streamed, as a client
// that reads one reply whole does. kind "resp" serves it on Responses,
// anything else on Anthropic.
func relayUpstreamWhole(t *testing.T, kind, model string, events ...string) (int, string) {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			io.WriteString(w, "data: "+ev+"\n\n")
		}
	}))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{model}}
	if kind == "resp" {
		p.Responses = up.URL + "/v1"
	} else {
		p.Anthropic = up.URL
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return post(t, "/v1/chat/completions", `{"model":"up/`+model+`","messages":[{"role":"user","content":"hi"}]}`)
}

// A reply cut short reaches a non-streaming client as it reaches a
// streaming one: as the error, not as the whole answer the cut masquerades
// as. Every translated reply's upstream is a stream, so an Anthropic one
// with no stop_reason and no message_stop, or a Responses one with no
// response.completed, would otherwise be rendered as the client's whole
// reply, a few words in.
func TestNonStreamEndedShort(t *testing.T) {
	const anthTextStart = `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`

	for _, tt := range []struct {
		name   string
		path   string
		model  string
		events []string
	}{
		{"anthropic", "", "grok-4", []string{anthStart("grok-4", 5), anthTextStart, anthText("Let me look at the")}},
		{"responses", "resp", "gpt-6.1", []string{
			`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"Let me look at the"}`,
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, body := relayUpstreamWhole(t, tt.path, tt.model, tt.events...)
			if !strings.Contains(body, "ended before it was complete") {
				t.Errorf("a cut reply served whole (status %d): %s", code, body)
			}
		})
	}
	// a reply the upstream said its last event of is served as it came
	code, body := relayUpstreamWhole(t, "", "grok-4", anthStart("grok-4", 5), anthTextStart, anthText("Done."), relayStop("end_turn"), relayEnd)
	if code != 200 || !strings.Contains(body, "Done.") || strings.Contains(body, "ended before") {
		t.Errorf("status %d: %s", code, body)
	}
	code, body = relayUpstreamWhole(t, "resp", "gpt-6.1",
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"Done."}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Done."}]}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":1}}}`)
	if code != 200 || !strings.Contains(body, "Done.") || strings.Contains(body, "ended before") {
		t.Errorf("status %d: %s", code, body)
	}
}

// The upstream's own stop reason is kept with the request, for a reply
// that ended too soon to be told apart by: the relay's word, not uni-switch's.
func TestRelayStopReasonRecorded(t *testing.T) {
	relayAnthropic(t, anthStart("grok-4", 5), anthText("Done."), relayStop("end_turn"), relayEnd)
	recs := usage.Load(time.Time{})
	if len(recs) != 1 || recs[0].Stop != "end_turn" {
		t.Fatalf("records %+v, want stop end_turn", recs)
	}
	// and a reply relayed as it came, to an agent of the same protocol
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{anthStart("grok-4", 5), anthText("Done."), relayStop("tool_calls"), relayEnd} {
			io.WriteString(w, "data:"+ev+"\r\n\r\n")
		}
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"grok-4"}, Anthropic: up.URL}); err != nil {
		t.Fatal(err)
	}
	if code, body := post(t, "/v1/messages", `{"model":"up/grok-4","stream":true,"max_tokens":9,"messages":[{"role":"user","content":"hi"}]}`); code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	recs = usage.Load(time.Time{})
	if len(recs) != 1 || recs[0].Stop != "tool_calls" {
		t.Fatalf("records %+v, want stop tool_calls", recs)
	}
}
