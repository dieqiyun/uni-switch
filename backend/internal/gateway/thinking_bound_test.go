package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ferock's refusal, as Command Code's plugin passed it on (Discord,
// 2026-10-30), up to where the report was cut.
const boundRefusalText = `messages.3.content.20: Invalid signature in thinking block. The block is bound to a different conversation. Remove the block, or set thinking.block_binding.prefix_mismatch_behavior to "drop_block". That setting requires the thinking-binding-controls-2026-08-01 value in the anthropic-beta header. The system`

// boundClaude answers as Claude does a conversation it signed only part
// of: the first thinking block whose signature is in foreign is refused,
// named where it is, in Anthropic's error body (uni-switch's reply puts the
// provider's name before it: "Command Code: {...}"); any other request is
// answered. said is what Claude refuses with (%d %d: message, block).
type boundClaude struct {
	mu      sync.Mutex
	bodies  []string
	foreign []string
	said    string
	// stream: the reply is Anthropic's stream, as a plugin's account wants
	stream bool
}

func (c *boundClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.bodies = append(c.bodies, string(b))
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	for mi, m := range gjson.GetBytes(b, "messages").Array() {
		for bi, blk := range m.Get("content").Array() {
			if blk.Get("type").String() == "thinking" && slices.Contains(c.foreign, blk.Get("signature").String()) {
				w.WriteHeader(400)
				msg := fmt.Sprintf(c.said, mi, bi)
				e, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": msg}})
				w.Write(e)
				return
			}
		}
	}
	if c.stream {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
		return
	}
	io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
}

func (c *boundClaude) sent() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.bodies)
}

// boundConversation is a Claude Code conversation whose message 3 has
// the refused block at 20, as ferock's had: thinking signed ok-<i> before
// it, other-20 there, and a later turn's thinking after it.
func boundConversation(model string) string {
	var blocks []string
	for i := 0; i < 20; i += 2 {
		blocks = append(blocks,
			fmt.Sprintf(`{"type":"thinking","thinking":"step %d","signature":"ok-%d"}`, i, i),
			fmt.Sprintf(`{"type":"text","text":"note %d"}`, i+1))
	}
	blocks = append(blocks,
		`{"type":"thinking","thinking":"from elsewhere","signature":"other-20"}`,
		`{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"a.go"}}`)
	return `{"model":"` + model + `","max_tokens":4096,"thinking":{"type":"adaptive"},"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"hi"}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"first","signature":"ok-a"},{"type":"text","text":"hello"}]},` +
		`{"role":"user","content":[{"type":"text","text":"read a.go"}]},` +
		`{"role":"assistant","content":[` + strings.Join(blocks, ",") + `]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package a"}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"later","signature":"later-5"},{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"b.go"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"package b"}]}]}`
}

// signatures lists a body's thinking signatures, in order.
func signatures(body string) []string {
	var out []string
	for _, m := range gjson.Get(body, "messages").Array() {
		for _, b := range m.Get("content").Array() {
			if b.Get("type").String() == "thinking" {
				out = append(out, b.Get("signature").String())
			}
		}
	}
	return out
}

func TestBoundRefusalRead(t *testing.T) {
	if !boundRefusal.MatchString(boundRefusalText) {
		t.Error("ferock's refusal isn't read as a bound thinking block")
	}
	for _, s := range []string{
		"messages.1.content.0: Invalid `signature` in `thinking` block",
		"messages.1.content.0: thinking block was not created in this conversation",
		"messages.1.content.0: Invalid data in redacted_thinking block",
	} {
		if !boundRefusal.MatchString(s) {
			t.Errorf("%q not read as a bound thinking block", s)
		}
	}
	for _, s := range []string{
		// DeepSeek's (#1611) and a plain bad request: nothing to leave out
		"The `content[].thinking` in the thinking mode must be passed back to the API",
		"messages.1.content.0: Expected `thinking` or `redacted_thinking`, but found `tool_use`",
		"prompt is too long",
	} {
		if boundRefusal.MatchString(s) {
			t.Errorf("%q read as a bound thinking block", s)
		}
	}
	if m := boundAt.FindStringSubmatch(boundRefusalText); m == nil || m[1] != "3" || m[2] != "20" {
		t.Errorf("boundAt = %v, want message 3 block 20", m)
	}
}

// ferock on Discord: Claude refused message 3's block 20 as bound to
// another conversation. uni-switch asks again without it and without the
// thinking after it, keeps the blocks before it, and sends the next turn
// so the first time.
func TestBoundThinkingLeftOut(t *testing.T) {
	fresh(t)
	up := &boundClaude{foreign: []string{"other-20"}, said: "messages.%d.content.%d: Invalid signature in thinking block. The block is bound to a different conversation. Remove the block, or set thinking.block_binding.prefix_mismatch_behavior to \"drop_block\"."}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	other := &boundClaude{}
	srv2 := httptest.NewServer(other)
	t.Cleanup(srv2.Close)
	for _, p := range []provider.Provider{
		{ID: "relay", Name: "Relay", Key: "k", Anthropic: srv.URL, Models: []string{"claude-opus-5-5"}},
		{ID: "relay2", Name: "Relay Two", Key: "k", Anthropic: srv2.URL, Models: []string{"claude-opus-5-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	postProto(t, s, "/v1/messages", boundConversation("relay/claude-opus-5-5"))
	sent := up.sent()
	if len(sent) != 2 {
		t.Fatalf("asked %d times, want once more after the refusal", len(sent))
	}
	before := []string{"ok-a", "ok-0", "ok-2", "ok-4", "ok-6", "ok-8", "ok-10", "ok-12", "ok-14", "ok-16", "ok-18"}
	if got := signatures(sent[1]); !slices.Equal(got, before) {
		t.Errorf("asked again with %v, want the blocks before the refused one: %v", got, before)
	}
	// the rest of the conversation is as the agent wrote it
	if n := len(gjson.Get(sent[1], "messages").Array()); n != 7 {
		t.Errorf("%d messages, want 7", n)
	}
	if c := gjson.Get(sent[1], "messages.3.content"); len(c.Array()) != 21 || c.Get("20.type").String() != "tool_use" {
		t.Errorf("message 3 = %s, want its blocks but the refused one", c.Raw)
	}
	if c := gjson.Get(sent[1], "messages.5.content"); len(c.Array()) != 1 || c.Get("0.id").String() != "t2" {
		t.Errorf("message 5 = %s, want its tool call alone", c.Raw)
	}

	// the next turn: the provider gets it without them, at once
	postProto(t, s, "/v1/messages", boundConversation("relay/claude-opus-5-5"))
	if sent = up.sent(); len(sent) != 3 {
		t.Fatalf("asked %d times more, want once", len(sent)-2)
	}
	if got := signatures(sent[2]); !slices.Equal(got, before) {
		t.Errorf("next turn sent %v, want %v", got, before)
	}

	// another provider never refused them: it gets every block
	postProto(t, s, "/v1/messages", boundConversation("relay2/claude-opus-5-5"))
	if got := other.sent(); len(got) != 1 || len(signatures(got[0])) != 13 {
		t.Errorf("another provider sent %d requests, signatures %v, want all 13 once", len(got), signatures(got[len(got)-1]))
	}
}

// A refusal over something else, DeepSeek's wanting its thinking back
// among them, is passed on as it came: one request, every block kept.
func TestOtherRefusalKeepsThinking(t *testing.T) {
	fresh(t)
	up := &boundClaude{foreign: []string{"other-20"}, said: "messages.%d.content.%d: The `content[].thinking` in the thinking mode must be passed back to the API"}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DS", Key: "k", Anthropic: srv.URL, Models: []string{"claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(boundConversation("ds/claude-opus-5-5")))
	req.Header.Set("anthropic-version", "2023-06-01")
	New().Handler().ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Fatalf("status 200, want the refusal")
	}
	if sent := up.sent(); len(sent) != 1 || len(signatures(sent[0])) != 13 {
		t.Errorf("asked %d times, want once with every block", len(sent))
	}
}

// withoutThinkingBlocks leaves a message that had only thinking out, and
// a body with nothing to leave out byte for byte.
func TestWithoutThinkingBlocks(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"redacted_thinking","data":"x"}]},{"role":"user","content":"go"}]}`)
	got := withoutThinkingBlocks(body, func(int, int, gjson.Result) bool { return true })
	if r := gjson.GetBytes(got, "messages.#.role").Raw; r != `["user","user"]` {
		t.Errorf("roles %s, want the empty assistant message gone", r)
	}
	if got := withoutThinkingBlocks(body, func(int, int, gjson.Result) bool { return false }); string(got) != string(body) {
		t.Errorf("changed with nothing to leave out: %s", got)
	}
}
