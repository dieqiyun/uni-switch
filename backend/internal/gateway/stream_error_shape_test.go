package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An upstream's stream that says it failed, in a shape its protocol's
// decoder can't read, or ends in another protocol's words, is a failed
// reply, never a completed one that said nothing: Codex took #1933's
// response.completed with no output as its turn's end, exit 0, and never
// used its stream retry. The upstream bodies are tensornull's fixtures,
// byte for byte where the report gives them.

// shapeUpstream serves body, as it is, as the event stream of a provider
// of the protocol proto, and counts the requests it gets.
func shapeUpstream(t *testing.T, proto provider.Protocol, body string) *atomic.Int32 {
	t.Helper()
	noRests()
	t.Cleanup(noRests)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var n atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "fixture", Name: "Fixture", Key: "unused-local-fixture-key", Models: []string{"claude-fixture"}}
	switch proto {
	case provider.Anthropic:
		p.Anthropic = up.URL
	case provider.Chat:
		p.Chat = up.URL + "/v1"
	case provider.Responses:
		p.Responses = up.URL + "/v1"
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return &n
}

const shapeRequest = `{"model":"fixture/claude-fixture","stream":true,"store":false,"max_output_tokens":32,` +
	`"input":[{"role":"user","content":"Return the fixture result."}]}`

// responsesEnd is a Responses stream's last event: its type, its status,
// its error's message and the text of its output.
func responsesEnd(body string) (typ, status, msg, text string) {
	evs := events(body)
	if len(evs) == 0 {
		return
	}
	last := evs[len(evs)-1]
	typ, _ = last["type"].(string)
	r, _ := last["response"].(map[string]any)
	status, _ = r["status"].(string)
	if e, ok := r["error"].(map[string]any); ok {
		msg, _ = e["message"].(string)
	}
	out, _ := r["output"].([]any)
	for _, x := range out {
		it, _ := x.(map[string]any)
		parts, _ := it["content"].([]any)
		for _, y := range parts {
			p, _ := y.(map[string]any)
			s, _ := p["text"].(string)
			text += s
		}
	}
	return
}

func TestMalformedStreamErrorsFailTheReply(t *testing.T) {
	cases := []struct {
		name  string
		proto provider.Protocol
		body  string
		msg   string // what the failure says
	}{
		{"anthropic error string", provider.Anthropic,
			"event: error\ndata: {\"type\": \"error\", \"error\": \"fixture upstream failure\"}\n\n",
			"fixture upstream failure"},
		{"anthropic error object", provider.Anthropic,
			"event: error\ndata: {\"type\": \"error\", \"error\": {\"type\": \"overloaded_error\", \"message\": \"fixture upstream failure\"}}\n\n",
			"fixture upstream failure"},
		{"anthropic error object with no message", provider.Anthropic,
			"event: error\ndata: {\"type\": \"error\", \"error\": {\"type\": \"overloaded_error\"}}\n\n",
			"overloaded_error"},
		{"anthropic events with no type", provider.Anthropic,
			"event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"model\":\"claude-fixture\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n" +
				"event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"fixture final answer\"}}\n\n" +
				"event: content_block_stop\ndata: {\"index\":0}\n\n" +
				"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
				"event: message_stop\ndata: {}\n\n",
			""},
		{"responses failure on the messages route", provider.Anthropic,
			"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_fixture\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"fixture upstream failure\"}}}\n\n",
			""},
		{"chat error string", provider.Chat,
			"data: {\"error\": \"fixture upstream failure\"}\n\n",
			"fixture upstream failure"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shapeUpstream(t, c.proto, c.body)
			code, body := post(t, "/backend-api/codex/responses", shapeRequest)
			typ, status, msg, _ := responsesEnd(body)
			if code == 200 && (typ != "response.failed" || status != "failed") {
				t.Fatalf("ended %s %q, want response.failed: %s", typ, status, body)
			}
			if c.msg != "" && !strings.Contains(msg+body, c.msg) {
				t.Errorf("failure says %q, want %q: %s", msg, c.msg, body)
			}
		})
	}
}

// A Responses upstream's, translated for a Messages client (one asked in
// Responses is passed its upstream's events as they are), is the same:
// an error event, no message_stop.
func TestMalformedResponsesErrorsFailTheReply(t *testing.T) {
	for name, c := range map[string]struct{ body, msg string }{
		"error string":         {"event: error\ndata: {\"type\": \"error\", \"error\": \"fixture upstream failure\"}\n\n", "fixture upstream failure"},
		"failed with no error": {"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_fixture\",\"status\":\"failed\"}}\n\n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			shapeUpstream(t, provider.Responses, c.body)
			code, body := post(t, "/v1/messages", `{"model":"fixture/claude-fixture","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"Return the fixture result."}]}`)
			var failed, stopped bool
			for _, ev := range events(body) {
				failed = failed || ev["type"] == "error"
				stopped = stopped || ev["type"] == "message_stop"
			}
			if code == 200 && (!failed || stopped) {
				t.Fatalf("error %v, message_stop %v, want an error and no stop: %s", failed, stopped, body)
			}
			if !strings.Contains(body, c.msg) {
				t.Errorf("failure doesn't say %q: %s", c.msg, body)
			}
		})
	}
}

// A non-streaming client is told the same: an error, not an empty reply.
func TestMalformedStreamErrorFailsUnstreamed(t *testing.T) {
	shapeUpstream(t, provider.Anthropic, "event: error\ndata: {\"type\": \"error\", \"error\": \"fixture upstream failure\"}\n\n")
	code, body := post(t, "/v1/responses", strings.Replace(shapeRequest, `"stream":true`, `"stream":false`, 1))
	if code < 400 || !strings.Contains(body, "fixture upstream failure") {
		t.Fatalf("status %d, want an error naming the upstream's: %s", code, body)
	}
}

// What the report asks to keep: a whole reply, and Claude's empty
// end_turn (after a tool's result, say), still complete.
func TestWellFormedStreamsStillComplete(t *testing.T) {
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-fixture\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n"
	end := "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	ping := "event: ping\ndata: {\"type\": \"ping\"}\n\n"
	text := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"fixture final answer\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	for name, c := range map[string]struct{ body, text string }{
		"text":           {start + ping + text + end, "fixture final answer"},
		"empty end_turn": {start + ping + end, ""},
	} {
		t.Run(name, func(t *testing.T) {
			n := shapeUpstream(t, provider.Anthropic, c.body)
			code, body := post(t, "/backend-api/codex/responses", shapeRequest)
			typ, status, _, got := responsesEnd(body)
			if code != 200 || typ != "response.completed" || status != "completed" || got != c.text {
				t.Fatalf("%d %s %q text %q, want completed with %q: %s", code, typ, status, got, c.text, body)
			}
			if n.Load() != 1 {
				t.Errorf("asked %d times, want once", n.Load())
			}
		})
	}
}
