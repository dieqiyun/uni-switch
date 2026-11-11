package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Reasoning state that goes from a client to an upstream of the same API
// through uni-switch's translation (a non-streaming request to a stream-only
// account, web search on the client's own API, a plugin, failover) comes
// back as the vendor wrote it (#1445, XYenon). The fixtures are the
// vendors' documented shapes.

// redactedBlock is a redacted_thinking block as Anthropic's extended
// thinking docs give it.
const redactedBlock = `{"type":"redacted_thinking","data":"EmwKAhgBEgy3va3pzix/LafPsn4aDFIT2Xlxh0L5L8rLVyIwxtE3rAFBa8cr3qpPkNRj2YfWXGmKDxH4mPnZ5sQ7vB5URj2pabUKZOJV4Dw=="}`

// translateStream runs an upstream's stream (its data lines) through the
// decoder of to and the encoder of from, as forwardTranslated's reply is.
func translateStream(t *testing.T, to, from provider.Protocol, lines ...string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	enc := makeEncoder(from, newSSEWriter(rec), &Request{Model: "m", Stream: true})
	dec := decoder(to)
	for _, l := range lines {
		if err := dec(l, enc.event); err != nil {
			t.Fatal(err)
		}
	}
	enc.finish()
	return sseData(t, rec.Body.Bytes())
}

// translateWhole runs an upstream's stream through the decoder of to and
// the collector, rendered for a from client that didn't stream.
func translateWhole(t *testing.T, to, from provider.Protocol, lines ...string) map[string]any {
	t.Helper()
	var c collector
	dec := decoder(to)
	for _, l := range lines {
		if err := dec(l, c.add); err != nil {
			t.Fatal(err)
		}
	}
	var out map[string]any
	b := render(from, c.finish(), &Request{Model: "m"})
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return out
}

// jsonIs says whether a value is the JSON want is, compacted.
func jsonIs(t *testing.T, got any, want string) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	if err := json.Compact(&w, []byte(want)); err != nil {
		t.Fatal(err)
	}
	// both through one decode, so key order is the same
	var gv, wv any
	json.Unmarshal(g, &gv)
	json.Unmarshal(w.Bytes(), &wv)
	a, _ := json.Marshal(gv)
	b, _ := json.Marshal(wv)
	return bytes.Equal(a, b)
}

// A Messages request's redacted_thinking goes back to Messages as it came,
// beside a thinking block signed with no text (display omitted) and one
// with text, each its own block. The old parse dropped the redacted block.
func TestAnthropicSealedThinkingGoesBackAsItCame(t *testing.T) {
	body := `{"model":"claude-opus-4-5","max_tokens":1024,"thinking":{"type":"enabled","budget_tokens":2048},"messages":[
		{"role":"user","content":"What's the weather in Paris?"},
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"","signature":"EqQBCkYIBxgCKkDomitted"},
			{"type":"thinking","thinking":"Let me check the weather.","signature":"ErUBCkYIBxgCIkB2signed"},
			` + redactedBlock + `,
			{"type":"tool_use","id":"toolu_01A09q90qw90lq917835lq9","name":"get_weather","input":{"location":"Paris"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01A09q90qw90lq917835lq9","content":"15 degrees"}]}]}`
	req, err := parse(provider.Anthropic, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	out, err := build(provider.Anthropic, req, "claude-opus-4-5", "api.anthropic.com", false)
	if err != nil {
		t.Fatal(err)
	}
	var q struct {
		Messages []struct {
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &q); err != nil || len(q.Messages) != 3 {
		t.Fatalf("%v: %s", err, out)
	}
	got := q.Messages[1].Content
	want := []string{
		`{"type":"thinking","thinking":"","signature":"EqQBCkYIBxgCKkDomitted"}`,
		`{"type":"thinking","thinking":"Let me check the weather.","signature":"ErUBCkYIBxgCIkB2signed"}`,
		redactedBlock,
		`{"type":"tool_use","id":"toolu_01A09q90qw90lq917835lq9","name":"get_weather","input":{"location":"Paris"}}`,
	}
	if len(got) != len(want) {
		t.Fatalf("assistant blocks %d, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if !jsonIs(t, got[i], want[i]) {
			t.Fatalf("block %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
	// the redacted block byte for byte
	if string(got[2]) != redactedBlock {
		t.Fatalf("redacted block changed:\n got %s\nwant %s", got[2], redactedBlock)
	}
}

// anthropicSealedStream is a Messages stream as the streaming docs give
// it: a thinking block signed with no text, one right after it with text,
// a redacted_thinking block, then the answer.
var anthropicSealedStream = []string{
	`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-opus-4-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"EqQBCkYIBxgCKkDomitted"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"Let me check the weather."}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"ErUBCkYIBxgCIkB2signed"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"content_block_start","index":2,"content_block":` + redactedBlock + `}`,
	`{"type":"content_block_stop","index":2}`,
	`{"type":"content_block_start","index":3,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"It's 15 degrees."}}`,
	`{"type":"content_block_stop","index":3}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":30}}`,
	`{"type":"message_stop"}`,
}

// A Messages stream through translation keeps each thinking block apart,
// signed as it was, and the redacted one whole. The old code merged the
// two thinking blocks' signatures into one and dropped the redacted block.
func TestAnthropicStreamKeepsSealedThinking(t *testing.T) {
	evs := translateStream(t, provider.Anthropic, provider.Anthropic, anthropicSealedStream...)
	type block struct {
		raw       any
		thinking  string
		signature string
	}
	var blocks []*block
	for _, ev := range evs {
		switch ev["type"] {
		case "content_block_start":
			cb := ev["content_block"].(map[string]any)
			b := &block{raw: cb}
			b.thinking, _ = cb["thinking"].(string)
			blocks = append(blocks, b)
		case "content_block_delta":
			d := ev["delta"].(map[string]any)
			b := blocks[len(blocks)-1]
			if s, ok := d["thinking"].(string); ok {
				b.thinking += s
			}
			if s, ok := d["signature"].(string); ok {
				b.signature += s
			}
		}
	}
	if len(blocks) != 4 {
		t.Fatalf("%d blocks, want 4: %v", len(blocks), evs)
	}
	if blocks[0].thinking != "" || blocks[0].signature != "EqQBCkYIBxgCKkDomitted" {
		t.Fatalf("signed block with no text: %+v", *blocks[0])
	}
	if blocks[1].thinking != "Let me check the weather." || blocks[1].signature != "ErUBCkYIBxgCIkB2signed" {
		t.Fatalf("signed block: %+v", *blocks[1])
	}
	if !jsonIs(t, blocks[2].raw, redactedBlock) {
		t.Fatalf("redacted block: %v", blocks[2].raw)
	}
}

// The same stream for a client that didn't stream: the reply's content
// has the three blocks as the stream gave them.
func TestAnthropicWholeReplyKeepsSealedThinking(t *testing.T) {
	out := translateWhole(t, provider.Anthropic, provider.Anthropic, anthropicSealedStream...)
	content, _ := out["content"].([]any)
	want := []string{
		`{"type":"thinking","thinking":"","signature":"EqQBCkYIBxgCKkDomitted"}`,
		`{"type":"thinking","thinking":"Let me check the weather.","signature":"ErUBCkYIBxgCIkB2signed"}`,
		redactedBlock,
		`{"type":"text","text":"It's 15 degrees."}`,
	}
	if len(content) != len(want) {
		t.Fatalf("content %d blocks, want %d: %v", len(content), len(want), content)
	}
	for i := range want {
		if !jsonIs(t, content[i], want[i]) {
			t.Fatalf("block %d: %v, want %s", i, content[i], want[i])
		}
	}
}

// A sealed block is nothing to another API's client: a Responses client
// isn't handed Anthropic's redacted data.
func TestAnthropicSealedThinkingStaysWithMessages(t *testing.T) {
	out := translateWhole(t, provider.Anthropic, provider.Responses, anthropicSealedStream...)
	b, _ := json.Marshal(out)
	if bytes.Contains(b, []byte("EmwKAhgBEgy3va3pzix")) || bytes.Contains(b, []byte("EqQBCkYIBxgCKkDomitted")) {
		t.Fatalf("Anthropic's sealed thinking reached a Responses client: %s", b)
	}
	evs := translateStream(t, provider.Anthropic, provider.Responses, anthropicSealedStream...)
	b, _ = json.Marshal(evs)
	if bytes.Contains(b, []byte("EmwKAhgBEgy3va3pzix")) {
		t.Fatalf("Anthropic's redacted thinking reached a Responses stream: %s", b)
	}
}

// Claude's subscription (Claude Code's stream) is the same sibling: a
// redacted block and two thinking blocks reach the client apart.
func TestClaudeSubscriptionKeepsSealedThinking(t *testing.T) {
	lines := []string{cutStart}
	for _, l := range anthropicSealedStream[1:] {
		lines = append(lines, `{"type":"stream_event","event":`+l+`}`)
	}
	lines = append(lines, `{"type":"result","subtype":"success","is_error":false,"result":"It's 15 degrees."}`)
	text, _, _, _, raw, _, _ := claudeReply(t, &subscriptionRun{}, lines, true)
	if text != "It's 15 degrees." {
		t.Fatalf("reply %q:\n%s", text, raw)
	}
	var starts []string
	for _, ev := range sseData(t, []byte(raw)) {
		if ev["type"] == "content_block_start" {
			b, _ := json.Marshal(ev["content_block"])
			starts = append(starts, string(b))
		}
	}
	if len(starts) != 4 || !jsonIs(t, json.RawMessage(starts[2]), redactedBlock) {
		t.Fatalf("blocks %v:\n%s", starts, raw)
	}
}
