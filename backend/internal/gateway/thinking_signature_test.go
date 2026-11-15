package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Grok Build reads a Messages thinking block as ContentBlock::Thinking
// {thinking, signature}, both required (its grok 1.0.67 binary), and
// turned a reply from minimax-code-global/MiniMax-M3.1-Flash away with
// "serialization error: missing field `signature`" (uk.jent on Discord).
//
// No bytes of MiniMax's reply were captured: there is no MiniMax Code
// account here. The unsigned shapes below are what MiniMax's Anthropic API
// docs read (`block.type == "thinking"`, `block.thinking`, thinking_delta;
// no signature or signature_delta named anywhere), and what a translated
// reply (Chat's reasoning_content) gave before.

// minimaxUnsignedStream is a Messages stream whose thinking block starts
// with no signature and gets none before its stop.
var minimaxUnsignedStream = sse(
	`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_mm1","type":"message","role":"assistant","model":"MiniMax-M3.1-Flash","content":[],"stop_reason":null,"usage":{"input_tokens":12,"output_tokens":0}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The user says hi. <greet back>"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello!"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":1}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":9}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`,
)

const minimaxUnsignedWhole = `{"id":"msg_mm2","type":"message","role":"assistant","model":"MiniMax-M3.1-Flash",` +
	`"content":[{"type":"thinking","thinking":"The user says hi. <greet back>"},{"type":"text","text":"Hello!"}],` +
	`"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":12,"output_tokens":9}}`

// thinkingBlocks are the thinking blocks a Messages reply gives its
// client: each stream's content_block_start, or a whole reply's content.
func thinkingBlocks(t *testing.T, body string) []map[string]any {
	t.Helper()
	var blocks []map[string]any
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		var reply struct {
			Content []map[string]any `json:"content"`
		}
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatalf("reply isn't JSON: %v\n%s", err, body)
		}
		for _, b := range reply.Content {
			if b["type"] == "thinking" {
				blocks = append(blocks, b)
			}
		}
		return blocks
	}
	for _, ev := range events(body) {
		if ev["type"] != "content_block_start" {
			continue
		}
		if b, _ := ev["content_block"].(map[string]any); b["type"] == "thinking" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// TestMessagesThinkingIsSigned: every thinking block uni-switch gives a
// Messages client says a signature, a string, whether the reply was
// relayed from a vendor's Messages endpoint that wrote none (MiniMax's) or
// translated from Chat's reasoning_content, streamed or whole. The
// thinking itself is unchanged, and what uni-switch adds is empty, never a
// made-up signature.
func TestMessagesThinkingIsSigned(t *testing.T) {
	for _, tt := range []struct {
		name   string
		proto  provider.Protocol
		stream bool
		f      *fake
	}{
		{"relayed stream", provider.Anthropic, true, &fake{reply: minimaxUnsignedStream}},
		{"relayed whole", provider.Anthropic, false, &fake{reply: minimaxUnsignedWhole, ctype: "application/json"}},
		{"translated stream", provider.Chat, true, &fake{reply: sse(glmChatThinksSlowly...)}},
		// the upstream is asked streamed; the client gets one body
		{"translated whole", provider.Chat, false, &fake{reply: sse(glmChatThinksSlowly...)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setup(t, tt.proto, tt.f)
			stream := "false"
			if tt.stream {
				stream = "true"
			}
			code, body := post(t, "/v1/messages", `{"model":"fake/m1","max_tokens":64,"stream":`+stream+`,"messages":[{"role":"user","content":"hi"}]}`)
			if code != 200 {
				t.Fatalf("status %d: %s", code, body)
			}
			blocks := thinkingBlocks(t, body)
			if len(blocks) != 1 {
				t.Fatalf("thinking blocks = %d, want 1:\n%s", len(blocks), body)
			}
			sig, ok := blocks[0]["signature"].(string)
			if !ok {
				t.Fatalf("thinking block has no signature string, as Grok Build turns away: %v\n%s", blocks[0], body)
			}
			if sig != "" {
				t.Errorf("signature = %q, want the empty one uni-switch adds", sig)
			}
			if _, ok := blocks[0]["thinking"].(string); !ok {
				t.Errorf("thinking block has no thinking string: %v", blocks[0])
			}
			if tt.proto == provider.Anthropic && !strings.Contains(body, "<greet back>") && !strings.Contains(body, `\u003cgreet back\u003e`) {
				t.Errorf("the thinking's text was lost:\n%s", body)
			}
		})
	}
}

// TestSignedThinkingRelayedAsItCame: a reply whose thinking is signed
// already (Anthropic's own) goes to the client byte for byte, its
// signature as Anthropic wrote it; only its model is the one asked for.
func TestSignedThinkingRelayedAsItCame(t *testing.T) {
	signed := strings.NewReplacer(
		`"content_block":{"type":"thinking","thinking":""}`, `"content_block":{"type":"thinking","thinking":"","signature":""}`,
		`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
		`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"EqQBCkYIBxgCKkDreal"}}`+"\n\n"+
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	).Replace(minimaxUnsignedStream)
	whole := strings.Replace(minimaxUnsignedWhole, `"thinking":"The user says hi. <greet back>"}`, `"thinking":"The user says hi. <greet back>","signature":"EqQBCkYIBxgCKkDreal"}`, 1)
	for _, tt := range []struct {
		name, reply, ctype, stream string
	}{
		{"stream", signed, "", "true"},
		{"whole", whole, "application/json", "false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setup(t, provider.Anthropic, &fake{reply: tt.reply, ctype: tt.ctype})
			code, body := post(t, "/v1/messages", `{"model":"fake/m1","max_tokens":64,"stream":`+tt.stream+`,"messages":[{"role":"user","content":"hi"}]}`)
			if code != 200 {
				t.Fatalf("status %d: %s", code, body)
			}
			if body != namedAs(tt.reply, "fake/m1") {
				t.Errorf("signed reply changed on its way:\n got %s\nwant %s", body, tt.reply)
			}
		})
	}
}

// TestSigFieldTidyCutAnywhere: a stream cut at any byte gives the same
// lines as one read whole, and a thinking block's text that mentions
// "thinking" in a delta is left alone.
func TestSigFieldTidyCutAnywhere(t *testing.T) {
	whole := (&sigFieldTidy{sse: true}).write([]byte(minimaxUnsignedStream))
	for cut := 1; cut < len(minimaxUnsignedStream); cut += 7 {
		tidy := &sigFieldTidy{sse: true}
		got := append(tidy.write([]byte(minimaxUnsignedStream[:cut])), tidy.write([]byte(minimaxUnsignedStream[cut:]))...)
		got = append(got, tidy.flush()...)
		if string(got) != string(whole) {
			t.Fatalf("cut at %d:\n got %s\nwant %s", cut, got, whole)
		}
	}
	if !strings.Contains(string(whole), `"content_block":{"signature":"","thinking":"","type":"thinking"}`) {
		t.Errorf("thinking start not signed:\n%s", whole)
	}
	if !strings.Contains(string(whole), `"thinking_delta","thinking":"The user says hi. <greet back>"}`) {
		t.Errorf("a delta changed:\n%s", whole)
	}
	// a null signature is none too
	if b, ok := unsignedThinking(json.RawMessage(`{"type":"thinking","thinking":"x","signature":null}`)); !ok || string(b) != `{"signature":"","thinking":"x","type":"thinking"}` {
		t.Errorf("null signature: %s, %v", b, ok)
	}
	// redacted_thinking keeps its data, untouched
	if _, ok := unsignedThinking(json.RawMessage(`{"type":"redacted_thinking","data":"abc"}`)); ok {
		t.Error("redacted_thinking was changed")
	}
}
