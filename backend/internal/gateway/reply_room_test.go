package gateway

import (
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

// deepseekWindow is a fake of DeepSeek's chat API that counts the reply
// asked for (max_tokens) in its window with the prompt, as DeepSeek does,
// and refuses in its words a request whose two together pass it.
type deepseekWindow struct {
	mu     sync.Mutex
	window int
	prompt int
	asked  []int64 // each request's max_tokens, -1 when it named none
}

func (d *deepseekWindow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	n := int64(-1)
	if v := gjson.GetBytes(b, "max_tokens"); v.Exists() {
		n = v.Int()
	}
	d.mu.Lock()
	d.asked = append(d.asked, n)
	d.mu.Unlock()
	if n < 0 || d.prompt+int(n) > d.window {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":{"message":"This model's maximum context length is %d tokens. However, you requested %d tokens (%d in the messages, %d in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`,
			d.window, d.prompt+int(n), d.prompt, n)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		`data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	))
}

func (d *deepseekWindow) took() []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := append([]int64(nil), d.asked...)
	d.asked = nil
	return out
}

// A client asking DeepSeek for its whole 384K reply on a conversation of
// 706,784 tokens is refused by DeepSeek as too long, since its 1M window
// counts the reply asked for: since #2355 passed max_completion_tokens on
// as max_tokens, every such turn at the official DeepSeek member of a
// group failed as "too long for its model" (molvqingtai on Discord). It
// is asked again for the reply the window has left, and answers. Room
// too small to be worth a reply still reaches the agent as the overflow.
func TestDeepSeekReplyFitsTheWindowLeft(t *testing.T) {
	fresh(t)
	d := &deepseekWindow{window: 1000000, prompt: 706784}
	up := httptest.NewServer(d)
	defer up.Close()
	p, err := provider.FromPreset("deepseek")
	if err != nil {
		t.Fatal(err)
	}
	p.Key = "fixture-key"
	p.Chat = up.URL + "/v1"
	// its chat API alone, the fake: no request reaches DeepSeek
	p.Responses, p.Anthropic = "", ""
	p.Models = []string{"deepseek-flash"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	msgs := `"messages":[{"role":"user","content":"hi"}]`
	for _, c := range []struct {
		path, body string
		want       []int64
	}{
		// DSH's pi-ai, OpenAI's field (#2355)
		{"/v1/chat/completions", `{"model":"deepseek/deepseek-flash","stream":true,"max_completion_tokens":393216,` + msgs + `}`, []int64{393216, 293216}},
		{"/v1/chat/completions", `{"model":"deepseek/deepseek-flash","stream":true,"max_tokens":393216,` + msgs + `}`, []int64{393216, 293216}},
		// a Responses client, translated to DeepSeek's chat API
		{"/v1/responses", `{"model":"deepseek/deepseek-flash","stream":true,"max_output_tokens":393216,"input":"hi"}`, []int64{393216, 293216}},
		// one that fits is asked once
		{"/v1/chat/completions", `{"model":"deepseek/deepseek-flash","stream":true,"max_tokens":32000,` + msgs + `}`, []int64{32000}},
	} {
		code, reply := post(t, c.path, c.body)
		if code != 200 {
			t.Fatalf("%s: HTTP %d: %s", c.body, code, reply)
		}
		if got := d.took(); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Fatalf("%s: DeepSeek was asked max_tokens %v, want %v", c.body, got, c.want)
		}
	}

	// the prompt leaves under leastReplyLeft: the conversation needs
	// compacting, and the agent is told it overflowed, asked once
	d.prompt = 1000000 - 4000
	code, reply := post(t, "/v1/chat/completions", `{"model":"deepseek/deepseek-flash","stream":true,"max_tokens":393216,`+msgs+`}`)
	if code != 400 || !strings.Contains(reply, "maximum context length") {
		t.Fatalf("no room: HTTP %d: %s", code, reply)
	}
	if got := d.took(); fmt.Sprint(got) != "[393216]" {
		t.Fatalf("no room: DeepSeek was asked %v, want once", got)
	}
}

func TestReplyLeft(t *testing.T) {
	for _, c := range []struct {
		msg  string
		want int
	}{
		{"This model's maximum context length is 1000000 tokens. However, you requested 1100000 tokens (706784 in the messages, 393216 in the completion). Please reduce the length of the messages or completion.", 293216},
		{"This model's maximum context length is 32768 tokens. However, you requested 32000 output tokens and your prompt contains at least 769 input tokens, for a total of at least 32769 tokens.", 31999},
		// Anthropic's, from an endpoint speaking its API
		{"input length and `max_tokens` exceed context limit: 970000 + 64000 > 1000000, decrease input length or `max_tokens` and try again", 30000},
		// the prompt alone is too long: nothing is left
		{"This model's maximum context length is 131072 tokens. However, you requested 140000 tokens (140000 in the messages, 0 in the completion).", 0},
		// says no parts
		{"This model's maximum context length is 131072 tokens. However, you requested 140000 tokens", 0},
		{"Invalid max_tokens value, the valid range of max_tokens is [1, 8192]", 0},
	} {
		if got := replyLeft([]byte(c.msg)); got != c.want {
			t.Errorf("replyLeft(%q) = %d, want %d", c.msg, got, c.want)
		}
	}
}

// Claude Code and Codex reach an endpoint speaking Anthropic's API (as
// DeepSeek's /anthropic does) with a reply limit of their own (Claude
// Code's 64000) or the model's whole (Codex names none, so uni-switch asks
// the model's, #1792). Told a 1M window, they compact only near it, and
// a turn whose prompt and reply together pass it was refused, so Codex
// stopped with "ran out of room in the model's context window" (da34,
// Jeremy.Zhou on Discord). Each is asked again for what the window left.
func TestAnthropicReplyFitsTheWindowLeft(t *testing.T) {
	fresh(t)
	const window, prompt = 1000000, 970000
	var mu sync.Mutex
	var asked []int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		n := gjson.GetBytes(b, "max_tokens").Int()
		mu.Lock()
		asked = append(asked, n)
		mu.Unlock()
		if prompt+int(n) > window {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"type":"error","error":{"type":"invalid_request_error","message":"input length and `+"`max_tokens`"+` exceed context limit: %d + %d > %d, decrease input length or `+"`max_tokens`"+` and try again"}}`, prompt, n, window)
			return
		}
		if gjson.GetBytes(b, "stream").Bool() {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Anthropic: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelOutput("a/m", 393216); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, c := range []struct {
		name, path, body string
		want             string
	}{
		{"Claude Code", "/v1/messages", `{"model":"a/m","max_tokens":64000,"messages":[{"role":"user","content":"hi"}]}`, "[64000 30000]"},
		{"Codex", "/v1/responses", `{"model":"a/m","input":"hi"}`, "[393216 30000]"},
	} {
		mu.Lock()
		asked = nil
		mu.Unlock()
		if code, raw := postJSON(t, s, c.path, c.body); code != 200 {
			t.Fatalf("%s: %d %s", c.name, code, raw)
		}
		mu.Lock()
		got := fmt.Sprint(asked)
		mu.Unlock()
		if got != c.want {
			t.Fatalf("%s: the endpoint was asked max_tokens %s, want %s", c.name, got, c.want)
		}
	}
}
