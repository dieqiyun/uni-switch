package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// GgbondHi6930 on X: a Smart group of DeepSeek and a Claude API key.
// The conversation ran on DeepSeek at max effort; with DeepSeek unticked
// its next turn went to Claude, which refused it, "signature in
// thinking". A new conversation worked. Claude is now sent the
// conversation without DeepSeek's thinking, so it never refuses it, and
// DeepSeek, ticked again, still gets every block.

// switchDeepSeek answers as DeepSeek's Anthropic endpoint does (checked
// against api.deepseek.com/anthropic on 2026-11-03): a thinking block
// signed with the reply's own id, a UUID, then a tool call or text.
type switchDeepSeek struct {
	mu     sync.Mutex
	n      int
	bodies []string
}

func (d *switchDeepSeek) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	d.mu.Lock()
	d.n++
	n := d.n
	d.bodies = append(d.bodies, string(b))
	d.mu.Unlock()
	id := fmt.Sprintf("49e144b6-799a-40e9-af71-%012d", n)
	var last string
	if ms := gjson.GetBytes(b, "messages").Array(); len(ms) > 0 {
		last = ms[len(ms)-1].Get("content.0.type").String()
	}
	act := fmt.Sprintf(`{"type":"tool_use","id":"call_00_ds%d","name":"Read","input":{"file_path":"f%d.go"}}`, n, n)
	stop := "tool_use"
	if last == "tool_result" {
		act, stop = `{"type":"text","text":"Read it."}`, "end_turn"
	}
	answerMessages(w, b, fmt.Sprintf(`{"id":%q,"type":"message","role":"assistant","model":"deepseek-v4-pro","content":[{"type":"thinking","thinking":"We need to read f%d.go.","signature":%q},%s],"stop_reason":%q,"stop_sequence":null,"usage":{"input_tokens":85,"output_tokens":18}}`, id, n, id, act, stop))
}

// answerMessages writes msg whole, or as the Messages stream when body asks for
// one: a thinking block starts with an empty signature, which its
// signature_delta then gives, as DeepSeek's and Claude's streams do.
func answerMessages(w http.ResponseWriter, body []byte, msg string) {
	if !gjson.GetBytes(body, "stream").Bool() {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, msg)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	m := gjson.Parse(msg)
	ev := func(name string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	}
	ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": m.Get("id").String(), "type": "message", "role": "assistant", "model": m.Get("model").String(), "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 1, "output_tokens": 0}}})
	for i, b := range m.Get("content").Array() {
		switch b.Get("type").String() {
		case "thinking":
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "thinking_delta", "thinking": b.Get("thinking").String()}})
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "signature_delta", "signature": b.Get("signature").String()}})
		case "text":
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": b.Get("text").String()}})
		case "tool_use":
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": b.Get("id").String(), "name": b.Get("name").String(), "input": map[string]any{}}})
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": b.Get("input").Raw}})
		}
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	ev("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": m.Get("stop_reason").String()}, "usage": map[string]any{"output_tokens": 1}})
	ev("message_stop", map[string]any{"type": "message_stop"})
}

// streamedContent is the content a Messages stream gives, as Claude Code
// puts it together.
func streamedContent(t *testing.T, stream string) string {
	t.Helper()
	var blocks []map[string]any
	inputs := map[int]string{}
	for _, line := range strings.Split(stream, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		e := gjson.Parse(data)
		i := int(e.Get("index").Int())
		switch e.Get("type").String() {
		case "content_block_start":
			var b map[string]any
			json.Unmarshal([]byte(e.Get("content_block").Raw), &b)
			for len(blocks) <= i {
				blocks = append(blocks, nil)
			}
			blocks[i] = b
		case "content_block_delta":
			d := e.Get("delta")
			switch d.Get("type").String() {
			case "text_delta":
				blocks[i]["text"] = blocks[i]["text"].(string) + d.Get("text").String()
			case "thinking_delta":
				blocks[i]["thinking"] = blocks[i]["thinking"].(string) + d.Get("thinking").String()
			case "signature_delta":
				blocks[i]["signature"] = d.Get("signature").String()
			case "input_json_delta":
				inputs[i] += d.Get("partial_json").String()
			}
		}
	}
	for i, in := range inputs {
		var v any
		if json.Unmarshal([]byte(in), &v) != nil {
			t.Fatalf("tool input %q isn't JSON", in)
		}
		blocks[i]["input"] = v
	}
	out, _ := json.Marshal(blocks)
	return string(out)
}

// switchClaude answers as api.anthropic.com does: a thinking block it
// didn't sign, in any turn, is refused with the first one named (checked
// on 2026-11-03 through OpenRouter's Anthropic route, a UUID-signed
// block in an earlier, finished turn too).
type switchClaude struct {
	mu     sync.Mutex
	bodies []string
	signed map[string]bool
	n      int
	// refused is how many requests Claude turned away
	refused int
}

func (c *switchClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, string(b))
	w.Header().Set("Content-Type", "application/json")
	for mi, m := range gjson.GetBytes(b, "messages").Array() {
		for bi, blk := range m.Get("content").Array() {
			if blk.Get("type").String() == "thinking" && !c.signed[blk.Get("signature").String()] {
				c.refused++
				w.WriteHeader(400)
				e, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": fmt.Sprintf("messages.%d.content.%d: Invalid `signature` in `thinking` block", mi, bi)}, "request_id": "req_011CgfP4zyRcUhPiTg5sLpoe"})
				w.Write(e)
				return
			}
		}
	}
	c.n++
	sig := fmt.Sprintf("EqQBCkgIBxABGAIiQL%d", c.n)
	if c.signed == nil {
		c.signed = map[string]bool{}
	}
	c.signed[sig] = true
	answerMessages(w, b, fmt.Sprintf(`{"id":"msg_011Cgf%d","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"Go on.","signature":%q},{"type":"text","text":"Done."}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`, c.n, sig))
}

// switchTurn posts a Claude Code turn to the group and gives back the
// conversation with the reply added, as Claude Code keeps it.
func switchTurn(t *testing.T, s *Server, stream bool, msgs []json.RawMessage) ([]json.RawMessage, int, string) {
	t.Helper()
	ms, _ := json.Marshal(msgs)
	body := `{"model":"group/dc","max_tokens":32000,"stream":` + fmt.Sprint(stream) + `,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},` +
		`"tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}}],` +
		`"metadata":{"user_id":"{\"session_id\":\"5c9a2f0e-1b7d-4c43-9a43-6f1e0a8b2d11\"}"},"messages":` + string(ms) + `}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("User-Agent", "claude-cli/2.1.120 (external, cli)")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		return msgs, rec.Code, rec.Body.String()
	}
	content := gjson.Get(rec.Body.String(), "content").Raw
	if stream {
		content = streamedContent(t, rec.Body.String())
	}
	return append(msgs, json.RawMessage(`{"role":"assistant","content":`+content+`}`)), 200, rec.Body.String()
}

// Only an upstream serving Claude is sent the body without DeepSeek's
// blocks; Claude's own, an unsigned one and a redacted one stay, and
// DeepSeek's endpoint and another vendor's get it byte for byte.
func TestWithoutForeignThinking(t *testing.T) {
	fresh(t)
	for _, p := range []provider.Provider{
		{ID: "claude-api", Name: "Claude API", Key: "k", Anthropic: "https://relay.example/anthropic", Models: []string{"claude-opus-5-5"}},
		{ID: "deepseek", Name: "DeepSeek", Key: "k", Anthropic: "https://api.deepseek.com/anthropic", Models: []string{"deepseek-v4-pro"}},
		{ID: "kimi", Name: "Kimi", Key: "k", Anthropic: "https://api.moonshot.ai/anthropic", Models: []string{"kimi-k2.6"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	find := func(id string) provider.Provider {
		p, err := provider.Find(id)
		if err != nil {
			t.Fatal(err)
		}
		return *p
	}
	conv := func(model string) []byte {
		return []byte(`{"model":"` + model + `","messages":[{"role":"user","content":"hi"},` +
			`{"role":"assistant","content":[{"type":"thinking","thinking":"a","signature":"49e144b6-799a-40e9-af71-4992a62c8076"},{"type":"text","text":"Hi"}]},` +
			`{"role":"user","content":"go"},` +
			`{"role":"assistant","content":[{"type":"thinking","thinking":"b","signature":"CAQS8gcKEAgTGAI4AUqAAQ=="},{"type":"thinking","thinking":"c","signature":""},{"type":"redacted_thinking","data":"EmwKAhgB"},{"type":"text","text":"Ok"}]},` +
			`{"role":"user","content":"more"}]}`)
	}
	got := withoutForeignThinking(find("claude-api"), conv("claude-opus-5-5"))
	if s := gjson.GetBytes(got, "messages.1.content.#.type").Raw; s != `["text"]` {
		t.Errorf("Claude's message 1 = %s, want DeepSeek's thinking out", s)
	}
	if s := gjson.GetBytes(got, "messages.3.content").Raw; s != gjson.GetBytes(conv("claude-opus-5-5"), "messages.3.content").Raw {
		t.Errorf("Claude's message 3 = %s, want it as it was", s)
	}
	for _, c := range []struct{ id, model string }{{"deepseek", "deepseek-v4-pro"}, {"kimi", "kimi-k2.6"}} {
		if got := withoutForeignThinking(find(c.id), conv(c.model)); string(got) != string(conv(c.model)) {
			t.Errorf("%s's body changed: %s", c.id, got)
		}
	}
	// nothing of DeepSeek's: byte for byte
	body := []byte(`{"model":"claude-opus-5-5","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"b","signature":"CAQS8gcKEAgTGAI4AUqAAQ=="}]}]}`)
	if got := withoutForeignThinking(find("claude-api"), body); string(got) != string(body) {
		t.Errorf("changed with nothing to leave out: %s", got)
	}
}
