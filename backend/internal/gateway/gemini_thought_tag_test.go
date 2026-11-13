package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// argonThoughtSSE is AI Studio's OpenAI-compatible stream for Gemini 4
// Argon asked for its thoughts, as #2098 captured it: the chunk opening
// <thought> is marked a thought, the one closing it, which goes on into
// the answer, isn't. Then the call the model made.
const argonThoughtSSE = `data: {"choices":[{"delta":{"role":"assistant","content":"<thought>[REASONING CONTENT REDACTED]","extra_content":{"google":{"thought":true}}},"index":0}],"id":"a1","model":"models/gemini-4-argon","object":"chat.completion.chunk"}

data: {"choices":[{"delta":{"role":"assistant","content":"</thought>接下来我将调用 probe"},"index":0}],"id":"a1","model":"models/gemini-4-argon","object":"chat.completion.chunk"}

data: {"choices":[{"delta":{"role":"assistant","content":" 工具读取编号 42 的健康状态。"},"index":0}],"id":"a1","model":"models/gemini-4-argon","object":"chat.completion.chunk"}

data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_probe","type":"function","function":{"name":"probe","arguments":"{\"number\":42}"}}]},"index":0}],"id":"a1","model":"models/gemini-4-argon","object":"chat.completion.chunk"}

data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":"tool_calls","index":0}],"id":"a1","model":"models/gemini-4-argon","object":"chat.completion.chunk","usage":{"prompt_tokens":120,"completion_tokens":40,"total_tokens":160}}

data: [DONE]

`

// #2098: Codex on Gemini 4 Argon over AI Studio's Chat API. The <thought>
// tags are left out of the answer, streamed and in the reply completed,
// the thought is reasoning, and the call comes through whole.
func TestGeminiMarkedThoughtTagLeftOut(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(argonThoughtSSE))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "google", Name: "Google", Key: "k", Chat: up.URL, Models: []string{"models/gemini-4-argon"}}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"google/models/gemini-4-argon","instructions":"你是一个助手。调用工具之前先向用户说明下一步操作，工具结果返回后给出简短总结。",
		"input":[{"role":"user","content":"请先用一句中文说明接下来的操作，然后调用 probe 工具读取编号 42 的健康状态。"}],
		"stream":true,"store":false,"max_output_tokens":8192,"reasoning":{"effort":"high","summary":"auto"},
		"tools":[{"type":"function","name":"probe","description":"Read the health status for one numbered test fixture.",
		"parameters":{"type":"object","properties":{"number":{"type":"integer"}},"required":["number"],"additionalProperties":false}}]}`
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var streamed, reasoning string
	var final []json.RawMessage
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		var ev struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Output []json.RawMessage `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			streamed += ev.Delta
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			reasoning += ev.Delta
		case "response.completed":
			final = ev.Response.Output
		}
	}
	const answer = "接下来我将调用 probe 工具读取编号 42 的健康状态。"
	if streamed != answer {
		t.Errorf("streamed %q, want %q", streamed, answer)
	}
	if reasoning != "[REASONING CONTENT REDACTED]" {
		t.Errorf("reasoning %q", reasoning)
	}
	out := ""
	for _, o := range final {
		out += string(o)
	}
	if strings.Contains(out, "thought>") || !strings.Contains(out, answer) || !strings.Contains(out, `"name":"probe"`) || !strings.Contains(out, `{\"number\":42}`) {
		t.Errorf("completed output %s", out)
	}
}

// Gemini's marked thoughts with the tags split anywhere, or none: the tags
// never reach the answer, and text that only quotes one keeps it.
func TestChatMarkedThoughtTags(t *testing.T) {
	type part struct {
		text   string
		marked bool
	}
	run := func(parts ...part) (think, text string) {
		var d chatDecoder
		for i, p := range parts {
			delta := map[string]any{"content": p.text}
			if p.marked {
				delta["extra_content"] = map[string]any{"google": map[string]any{"thought": true}}
			}
			ch := map[string]any{"index": 0, "delta": delta}
			if i == len(parts)-1 {
				ch["finish_reason"] = "stop"
			}
			b, _ := json.Marshal(map[string]any{"id": "x", "choices": []any{ch}})
			d.decode(string(b), func(ev Event) {
				switch ev.Kind {
				case KThink:
					think += ev.Text
				case KText:
					text += ev.Text
				}
			})
		}
		return
	}
	for _, c := range []struct {
		name        string
		parts       []part
		think, text string
	}{
		{"close unmarked", []part{{"<thought>hm", true}, {"</thought>Hi", false}}, "hm", "Hi"},
		{"more thought marked", []part{{"<thought>a", true}, {"b", true}, {"</thought>\nHi", false}}, "ab", "Hi"},
		{"open split", []part{{"<tho", true}, {"ught>a", true}, {"</thought>Hi", false}}, "a", "Hi"},
		{"close split", []part{{"<thought>a", true}, {"</tho", false}, {"ught>Hi", false}}, "a", "Hi"},
		{"close marked", []part{{"<thought>a</thought>", true}, {"Hi", false}}, "a", "Hi"},
		{"only the close", []part{{"<thought>a", true}, {"</thought>", false}, {"Hi", false}}, "a", "Hi"},
		{"no tags", []part{{"**Plan**\n\nthink", true}, {"Hi", false}}, "**Plan**\n\nthink", "Hi"},
		{"no tags, then a quoted tag", []part{{"think", true}, {"Use <thought> here", false}}, "think", "Use <thought> here"},
		{"answer quotes a close", []part{{"think", true}, {"Hi", false}, {" </thought>", false}}, "think", "Hi </thought>"},
	} {
		t.Run(c.name, func(t *testing.T) {
			think, text := run(c.parts...)
			if think != c.think || text != c.text {
				t.Errorf("think %q text %q, want %q %q", think, text, c.think, c.text)
			}
		})
	}
}
