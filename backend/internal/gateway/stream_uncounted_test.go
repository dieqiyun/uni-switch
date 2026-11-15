package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2086 (tensornull): a Codex turn on an Anthropic-compatible proxy ended
// as response.completed with no output, after 21s, and the route had no
// usage and no served model: the upstream said its reply ended but never
// counted a token, which no real Claude or Responses reply does (Claude's
// empty end_turn after a tool's result still counts its prompt). Told
// whole, Codex ended its turn with last_agent_message null and didn't ask
// again. It is the empty reply that is asked again (#667), and failed if
// it stays empty.
const responsesUncounted = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
	"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n"

var uncountedEnds = map[string]struct {
	proto provider.Protocol
	body  string
}{
	"message_stop only": {provider.Anthropic,
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
	"pings then message_stop": {provider.Anthropic,
		"event: ping\ndata: {\"type\": \"ping\"}\n\n" +
			"event: ping\ndata: {\"type\": \"ping\"}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
	"stop_reason with no usage": {provider.Anthropic,
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
	"zero usage": {provider.Anthropic,
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-fixture\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
}

func TestUncountedEmptyReplyFails(t *testing.T) {
	for name, c := range uncountedEnds {
		t.Run(name, func(t *testing.T) {
			n := shapeUpstream(t, c.proto, c.body)
			code, body := post(t, "/backend-api/codex/responses", shapeRequest)
			typ, status, msg, _ := responsesEnd(body)
			if code == 200 && (typ != "response.failed" || status != "failed") {
				t.Fatalf("ended %s %q, want response.failed: %s", typ, status, body)
			}
			if !strings.Contains(msg+body, "empty reply") {
				t.Errorf("failure says %q, want the empty reply's words", msg)
			}
			if n.Load() < 2 {
				t.Errorf("asked %d times, want it asked again before failing", n.Load())
			}
		})
	}
}

// A Responses upstream's, translated for a Messages client, is the same.
// (Codex asking a Responses provider is passed its events as they are,
// which this doesn't change.)
func TestUncountedResponsesReplyFails(t *testing.T) {
	n := shapeUpstream(t, provider.Responses, responsesUncounted)
	code, body := post(t, "/v1/messages", `{"model":"fixture/claude-fixture","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"Return the fixture result."}]}`)
	if code == 200 && (!strings.Contains(body, "empty reply") || strings.Contains(body, "message_stop")) {
		t.Fatalf("status %d, want the empty reply's error and no message_stop: %s", code, body)
	}
	if n.Load() < 2 {
		t.Errorf("asked %d times, want it asked again before failing", n.Load())
	}
}

// A Messages and a Chat client are told the same, streamed or not.
func TestUncountedEmptyReplyFailsEveryClient(t *testing.T) {
	c := uncountedEnds["stop_reason with no usage"]
	msgs := `{"model":"fixture/claude-fixture","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"Return the fixture result."}]}`
	for name, ask := range map[string]struct{ path, body string }{
		"chat":                 {"/v1/chat/completions", msgs},
		"responses unstreamed": {"/v1/responses", strings.Replace(shapeRequest, `"stream":true`, `"stream":false`, 1)},
		"chat unstreamed":      {"/v1/chat/completions", strings.Replace(msgs, `"stream":true`, `"stream":false`, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			shapeUpstream(t, c.proto, c.body)
			code, body := post(t, ask.path, ask.body)
			if code == 200 && !strings.Contains(body, "empty reply") {
				t.Fatalf("status %d, want the empty reply's failure: %s", code, body)
			}
		})
	}
}

// Replies that count their tokens and say nothing are still whole: Claude's
// empty end_turn (TestWellFormedStreamsStillComplete) and a Responses
// reply's.
func TestCountedEmptyReplyStillCompletes(t *testing.T) {
	shapeUpstream(t, provider.Responses,
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n"+
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":9,\"output_tokens\":1,\"total_tokens\":10}}}\n\n")
	code, body := post(t, "/v1/messages", `{"model":"fixture/claude-fixture","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"Return the fixture result."}]}`)
	if code != 200 || !strings.Contains(body, "message_stop") || strings.Contains(body, "empty reply") {
		t.Fatalf("status %d, want a whole empty reply: %s", code, body)
	}
}
