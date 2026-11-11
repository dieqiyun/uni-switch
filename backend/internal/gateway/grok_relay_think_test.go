package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// grokRelaySearchStream is a reply as grok2api_python (xLmiler's Grok
// reverse proxy) streams its grok-3-search model with search results
// shown: the results come first, as json.dumps writes them, between
// <think> tags led by a Windows line end ("\r\n<think>{results}</think>\r\n"
// in process_model_response), then Grok's answer token by token, with no
// finish_reason before [DONE].
const grokRelaySearchStream = `data: {"id": "chatcmpl-6c1f0e3a-1b7e-4a51-9a51-0c6e8f1f2d11", "created": 1761700000, "model": "grok-3-search", "object": "chat.completion.chunk", "choices": [{"index": 0, "delta": {"content": "\r\n<think>\r\n<details><summary>\u8d44\u6599[0]: uni-switch</summary>\r\nA local LLM gateway\r\n\n[Link](https://github.com/dieqiyun/uni-switch)\r\n</details></think>\r\n"}}]}

data: {"id": "chatcmpl-1f0b2c4d-8e9a-4b3c-a2d1-5e6f7a8b9c0d", "created": 1761700000, "model": "grok-3-search", "object": "chat.completion.chunk", "choices": [{"index": 0, "delta": {"content": "uni-switch"}}]}

data: {"id": "chatcmpl-2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d", "created": 1761700000, "model": "grok-3-search", "object": "chat.completion.chunk", "choices": [{"index": 0, "delta": {"content": " is a gateway."}}]}

data: [DONE]

`

// A Grok relay that puts a line end before its leading <think> block
// (jenryai on X: the thinking came out as the answer's text) has that
// block taken as thinking on every route a translated client takes,
// streamed to it or not, and the answer starts at its first word.
func TestGrokRelayThinkAfterLineEnd(t *testing.T) {
	for _, c := range []struct {
		name, path, body string
	}{
		{"messages", "/v1/messages", `{"model":"grokr/grok-3-search","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"uni-switch?"}]}`},
		{"responses", "/v1/responses", `{"model":"grokr/grok-3-search","stream":true,"input":[{"role":"user","content":"uni-switch?"}]}`},
		{"messages whole", "/v1/messages", `{"model":"grokr/grok-3-search","max_tokens":100,"messages":[{"role":"user","content":"uni-switch?"}]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			up := httptest.NewServer(&fake{t: t, reply: grokRelaySearchStream})
			t.Cleanup(up.Close)
			if err := provider.Save(provider.Provider{ID: "grokr", Name: "Grok relay", Key: "k", Models: []string{"grok-3-search"}, Chat: up.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			code, out := post(t, c.path, c.body)
			if code != 200 {
				t.Fatalf("code %d: %s", code, out)
			}
			think, text := grokRelayParts(t, out)
			if !strings.Contains(think, "资料[0]: uni-switch") || !strings.Contains(think, "[Link](https://github.com/dieqiyun/uni-switch)") {
				t.Errorf("thinking %q, want the search results", think)
			}
			if text != "uni-switch is a gateway." {
				t.Errorf("answer %q, want %q", text, "uni-switch is a gateway.")
			}
		})
	}
}

// grokRelayParts is the thinking and the answer's text in a Messages or
// Responses reply, streamed or whole.
func grokRelayParts(t *testing.T, out string) (think, text string) {
	t.Helper()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		for _, ev := range events("data: " + out + "\n\n") {
			blocks, _ := ev["content"].([]any)
			for _, b := range blocks {
				m, _ := b.(map[string]any)
				if v, ok := m["thinking"].(string); ok {
					think += v
				}
				if v, ok := m["text"].(string); ok {
					text += v
				}
			}
		}
		return
	}
	for _, ev := range events(out) {
		switch ev["type"] {
		case "content_block_delta":
			d, _ := ev["delta"].(map[string]any)
			if v, ok := d["thinking"].(string); ok {
				think += v
			}
			if v, ok := d["text"].(string); ok {
				text += v
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			think += ev["delta"].(string)
		case "response.output_text.delta":
			text += ev["delta"].(string)
		}
	}
	return
}
