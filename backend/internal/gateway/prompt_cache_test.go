package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexTurn is a Codex request as it reaches uni-switch: its instructions, its
// developer and context messages, the conversation so far, its tools and its
// thread's id as prompt_cache_key.
func codexTurn(input string) string {
	return `{"model":"m1","stream":true,"store":false,"instructions":"you are codex","prompt_cache_key":"thread-1",
	  "input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions>"}]},
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>"}]},
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]}` + input + `],
	  "tools":[{"type":"function","name":"shell","description":"run","parameters":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"string"}}}}],
	  "reasoning":{"effort":"high","summary":"auto"},"include":["reasoning.encrypted_content"],"parallel_tool_calls":true,"tool_choice":"auto"}`
}

var deepseekReply = sse(
	`data: {"id":"c1","model":"m1","choices":[{"delta":{"reasoning_content":"think"}}]}`,
	`data: {"id":"c1","model":"m1","choices":[{"delta":{"content":"ok"}}]}`,
	`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
	`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":3000,"completion_tokens":2,"prompt_cache_hit_tokens":2048,"prompt_cache_miss_tokens":952}}`,
	`data: [DONE]`)

func completedUsage(t *testing.T, body string) map[string]any {
	t.Helper()
	for _, e := range events(body) {
		if e["type"] == "response.completed" {
			return e["response"].(map[string]any)["usage"].(map[string]any)
		}
	}
	t.Fatalf("no response.completed: %s", body)
	return nil
}

// A Codex thread on a Chat Completions upstream: each turn's request starts
// with the last one's, byte for byte, so the upstream's prefix cache is
// read; Codex's prompt_cache_key goes along for relays that route by it; and
// what the upstream read from its cache reaches Codex as cached_tokens.
func TestCodexThreadOnChatUpstreamCaches(t *testing.T) {
	f := &fake{t: t, reply: deepseekReply}
	setup(t, provider.Chat, f)
	code, body := post(t, "/v1/responses", codexTurn(""))
	if code != 200 {
		t.Fatalf("turn 1: %d %s", code, body)
	}
	first := append([]byte(nil), f.got...)
	if u := completedUsage(t, body); u["input_tokens"] != float64(3000) ||
		u["input_tokens_details"].(map[string]any)["cached_tokens"] != float64(2048) {
		t.Errorf("usage to Codex: %v", u)
	}
	code, body = post(t, "/v1/responses", codexTurn(`,
	    {"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"think"}],"encrypted_content":null},
	    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]},
	    {"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"z\":\"1\",\"a\":\"2\"}"},
	    {"type":"function_call_output","call_id":"call_1","output":"x"}`))
	if code != 200 {
		t.Fatalf("turn 2: %d %s", code, body)
	}
	var a, b struct {
		Messages []json.RawMessage `json:"messages"`
		Tools    json.RawMessage   `json:"tools"`
		Key      string            `json:"prompt_cache_key"`
	}
	json.Unmarshal(first, &a)
	json.Unmarshal(f.got, &b)
	if a.Key != "thread-1" || b.Key != "thread-1" {
		t.Errorf("prompt_cache_key: %q %q", a.Key, b.Key)
	}
	if len(b.Messages) <= len(a.Messages) || !bytes.Equal(a.Tools, b.Tools) {
		t.Fatalf("turns:\n%s\n%s", first, f.got)
	}
	for i := range a.Messages {
		if !bytes.Equal(a.Messages[i], b.Messages[i]) {
			t.Errorf("message %d changed between turns:\n%s\n%s", i, a.Messages[i], b.Messages[i])
		}
	}
}

// An upstream that turns away the unknown prompt_cache_key is asked again
// without it, and not sent it again.
func TestPromptCacheKeyRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		refuse func(body []byte) (int, string)
		codes  []int // each turn's status to Codex
		calls  []int // upstream calls each turn
		keyed  bool  // the key still goes upstream after
	}{
		{"names it", func(body []byte) (int, string) {
			if bytes.Contains(body, []byte(`"prompt_cache_key"`)) {
				return 400, `{"error":{"message":"Invalid JSON payload received. Unknown name \"prompt_cache_key\": Cannot find field."}}`
			}
			return 0, ""
		}, []int{200, 200}, []int{2, 1}, false},
		{"doesn't name it", func(body []byte) (int, string) {
			if bytes.Contains(body, []byte(`"prompt_cache_key"`)) {
				return 422, `{"error":{"message":"invalid request body"}}`
			}
			return 0, ""
		}, []int{200, 200}, []int{2, 1}, false},
		{"refused for something else", func(body []byte) (int, string) {
			return 400, `{"error":{"message":"context too long"}}`
		}, []int{400, 400}, []int{2, 2}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{t: t, reply: deepseekReply, refuse: c.refuse}
			setup(t, provider.Chat, f)
			srv := New()
			for turn := range 2 {
				calls := f.calls
				code, body := postTo(t, srv, "/v1/responses", codexTurn(""))
				if code != c.codes[turn] || code == 200 && !strings.Contains(body, "response.completed") {
					t.Fatalf("turn %d: %d %s", turn+1, code, body)
				}
				if n := f.calls - calls; n != c.calls[turn] {
					t.Errorf("turn %d: %d upstream calls, want %d", turn+1, n, c.calls[turn])
				}
			}
			if got := srv.fits("fake", cacheKeyField, provider.Chat); got != c.keyed {
				t.Errorf("key still sent: %v, want %v", got, c.keyed)
			}
		})
	}
}

func postTo(t *testing.T, srv *Server, path, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// What Chat Completions vendors and relays say was read from, or written
// to, their cache, whichever names they give it.
func TestChatUsageCache(t *testing.T) {
	for _, c := range []struct {
		name, usage string
		want        Usage
	}{
		{"openai", `{"prompt_tokens":1000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":800}}`, Usage{Input: 200, Output: 5, CacheRead: 800}},
		{"deepseek", `{"prompt_tokens":1000,"completion_tokens":5,"prompt_cache_hit_tokens":900,"prompt_cache_miss_tokens":100}`, Usage{Input: 100, Output: 5, CacheRead: 900}},
		{"deepseek both", `{"prompt_tokens":1000,"completion_tokens":5,"prompt_cache_hit_tokens":900,"prompt_tokens_details":{"cached_tokens":900}}`, Usage{Input: 100, Output: 5, CacheRead: 900}},
		{"moonshot", `{"prompt_tokens":1000,"completion_tokens":5,"cached_tokens":600}`, Usage{Input: 400, Output: 5, CacheRead: 600}},
		{"openrouter", `{"prompt_tokens":1000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":700}}`, Usage{Input: 300, Output: 5, CacheWrite: 700}},
		{"claude relay, whole", `{"prompt_tokens":1000,"completion_tokens":5,"cache_read_input_tokens":800,"cache_creation_input_tokens":100}`, Usage{Input: 100, Output: 5, CacheRead: 800, CacheWrite: 100}},
		{"claude relay, Anthropic's count", `{"prompt_tokens":10,"completion_tokens":5,"cache_read_input_tokens":800,"cache_creation_input_tokens":100}`, Usage{Input: 10, Output: 5, CacheRead: 800, CacheWrite: 100}},
		{"none", `{"prompt_tokens":1000,"completion_tokens":5}`, Usage{Input: 1000, Output: 5}},
	} {
		var u cUsage
		if err := json.Unmarshal([]byte(c.usage), &u); err != nil {
			t.Fatal(err)
		}
		if got := u.usage(); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A Codex request sent to Claude is marked for Anthropic's prompt cache:
// its instructions, and its conversation at the last block that can be.
func TestAnthropicPromptCache(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m","instructions":"be brief","stream":true,
	  "tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
	  "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]},
	    {"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
	    {"type":"function_call_output","call_id":"c1","output":"a.go b.go"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		System []aBlock `json:"system"`
		Tools  []map[string]any
		Msgs   []struct{ Content []aBlock } `json:"messages"`
	}
	b := buildAnthropic(r, "claude-sonnet-5")
	json.Unmarshal(b, &out)
	if len(out.System) != 1 || out.System[0].Text != "be brief" || out.System[0].CacheControl["type"] != "ephemeral" {
		t.Fatalf("system: %s", b)
	}
	last := out.Msgs[len(out.Msgs)-1].Content
	if lb := last[len(last)-1]; lb.Type != "tool_result" || lb.CacheControl["type"] != "ephemeral" {
		t.Fatalf("last block: %s", b)
	}
	if n := strings.Count(string(b), `"cache_control"`); n != 2 {
		t.Fatalf("%d marks: %s", n, b)
	}

	// no system prompt: the tools are marked instead; thinking never is
	r.System = ""
	r.Messages = append(r.Messages, Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: "done"}, {Kind: Thinking, Text: "hm", Signature: "sig"}}})
	b = buildAnthropic(r, "claude-sonnet-5")
	out.System, out.Tools, out.Msgs = nil, nil, nil
	json.Unmarshal(b, &out)
	last = out.Msgs[len(out.Msgs)-1].Content
	if out.System != nil || out.Tools[0]["cache_control"] == nil || last[1].Type != "thinking" || last[1].CacheControl != nil || last[0].CacheControl == nil {
		t.Fatalf("no system: %s", b)
	}
	if n := strings.Count(string(b), `"cache_control"`); n != 2 {
		t.Fatalf("%d marks: %s", n, b)
	}
}
