package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"
)

// ollamaUpstream is a Chat Completions provider that keeps each request
// it is sent in *got (when got isn't nil) and answers with reasoning,
// text and a tool call split over chunks, then usage, streamed or whole
// as asked.
func ollamaUpstream(t *testing.T, got *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			mu.Lock()
			*got = append(*got, string(b))
			mu.Unlock()
		}
		if !gjson.GetBytes(b, "stream").Bool() {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"seer","choices":[{"index":0,"message":{"role":"assistant","content":"whole answer","reasoning_content":"pondered","tool_calls":[{"id":"call_up","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range []string{
			`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"let me "}}]}`,
			`{"choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":", world"}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_up","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.go\"}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`,
		} {
			io.WriteString(w, "data: "+c+"\n\n")
			w.(http.Flusher).Flush()
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	return up
}

// ollamaCall posts body to path as a client on this computer would, through
// the whole guarded stack.
func ollamaCall(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.RemoteAddr, r.Host = "127.0.0.1:50123", "127.0.0.1:3425"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// ndjson is each line of an NDJSON reply.
func ndjson(t *testing.T, body string) []gjson.Result {
	t.Helper()
	var out []gjson.Result
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		if !json.Valid(sc.Bytes()) {
			t.Fatalf("not a JSON line: %q in %s", sc.Text(), body)
		}
		out = append(out, gjson.Parse(sc.Text()))
	}
	return out
}

// Zed's Ollama provider (super_lalalalala on Discord) chats on
// /api/chat, which uni-switch answered 404: it is served now, as
// /v1/chat/completions is, with Zed's own request (crates/ollama
// ChatRequest: stream true, keep_alive, options num_ctx/num_predict/
// temperature, think, tools; an assistant's tool call with an id, a tool
// result named by tool_name alone; a user image as bare base64). The
// provider is sent it as Chat Completions, and the reply comes back as
// Ollama's NDJSON: thinking and text as they arrive, the tool call whole
// with its arguments an object and an id, and a last done line with the
// reason and the token counts.
func TestOllamaChatStreams(t *testing.T) {
	fresh(t)
	var got []string
	ollamaPlan(t, ollamaUpstream(t, &got).URL+"/v1")
	h := localGuard(New().Handler())
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	body := `{"model":"plan/seer","messages":[` +
		`{"role":"system","content":"You are Zed."},` +
		`{"role":"user","content":"look","images":["` + png + `"]},` +
		`{"role":"assistant","content":"","tool_calls":[{"id":"t1","function":{"name":"read_file","arguments":{"path":"x.go"}}},{"function":{"name":"grep","arguments":{"q":"y"}}}],"thinking":"hm"},` +
		`{"role":"tool","tool_name":"grep","content":"no match"},` +
		`{"role":"tool","tool_name":"read_file","content":"package x"}` +
		`],"stream":true,"keep_alive":"5m","options":{"num_ctx":32768,"num_predict":512,"temperature":0.3,"top_k":40,"stop":["</end>"]},` +
		`"tools":[{"type":"function","function":{"name":"read_file","description":"Read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],"think":true}`
	w := ollamaCall(t, h, "/api/chat", body)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("chat: %d %s %s", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	if len(got) != 1 {
		t.Fatalf("upstream asked %d times", len(got))
	}
	up := gjson.Parse(got[0])
	for k, want := range map[string]string{
		"model": "seer", "stream": "true", "stream_options.include_usage": "true", "max_tokens": "512", "temperature": "0.3", "stop.0": "</end>",
		"messages.0.role": "system", "messages.0.content": "You are Zed.",
		"messages.1.content.0.text": "look", "messages.1.content.1.image_url.url": "data:image/png;base64," + png,
		"messages.2.tool_calls.0.id": "t1", "messages.2.tool_calls.0.function.arguments": `{"path":"x.go"}`,
		"messages.2.tool_calls.1.function.name": "grep", "messages.2.reasoning_content": "hm",
		"messages.3.role": "tool", "messages.3.content": "no match",
		"messages.4.tool_call_id": "t1", "messages.4.content": "package x",
		"tools.0.function.name": "read_file",
	} {
		if g := up.Get(k).String(); g != want {
			t.Errorf("upstream %s = %q, want %q (sent %s)", k, g, want, got[0])
		}
	}
	if id := up.Get("messages.3.tool_call_id").String(); id == "" || id != up.Get("messages.2.tool_calls.1.id").String() {
		t.Errorf("grep's result went with %q, its call has id %q", id, up.Get("messages.2.tool_calls.1.id"))
	}
	for _, k := range []string{"num_ctx", "top_k", "keep_alive", "options", "think", "images"} {
		if up.Get(k).Exists() {
			t.Errorf("upstream was sent Ollama's own %s: %s", k, got[0])
		}
	}

	lines := ndjson(t, w.Body.String())
	var text, think string
	var calls []gjson.Result
	for _, l := range lines[:len(lines)-1] {
		if l.Get("done").Bool() || l.Get("model").String() != "plan/seer" || l.Get("message.role").String() != "assistant" || !l.Get("message.content").Exists() || !l.Get("created_at").Exists() {
			t.Fatalf("part %s", l.Raw)
		}
		text += l.Get("message.content").String()
		think += l.Get("message.thinking").String()
		calls = append(calls, l.Get("message.tool_calls").Array()...)
	}
	if text != "Hello, world" || think != "let me think" {
		t.Errorf("text %q thinking %q", text, think)
	}
	if len(calls) != 1 || calls[0].Get("id").String() != "call_up" || calls[0].Get("function.name").String() != "read_file" || calls[0].Get("function.arguments").Raw != `{"path":"a.go"}` {
		t.Errorf("tool calls %v", calls)
	}
	last := lines[len(lines)-1]
	if !last.Get("done").Bool() || last.Get("done_reason").String() != "stop" || last.Get("prompt_eval_count").Int() != 11 || last.Get("eval_count").Int() != 7 || last.Get("message.content").String() != "" {
		t.Errorf("done line %s", last.Raw)
	}
}

// "stream": false gets one JSON object, the whole reply in it; and with
// think false a model's reasoning isn't told.
func TestOllamaChatWhole(t *testing.T) {
	fresh(t)
	var got []string
	ollamaPlan(t, ollamaUpstream(t, &got).URL+"/v1")
	h := localGuard(New().Handler())
	w := ollamaCall(t, h, "/api/chat", `{"model":"plan/seer","messages":[{"role":"user","content":"hi"}],"stream":false,"format":"json","think":"high"}`)
	if w.Code != 200 || len(got) != 1 {
		t.Fatalf("chat: %d %s (upstream %d)", w.Code, w.Body, len(got))
	}
	if up := gjson.Parse(got[0]); up.Get("stream").Bool() || up.Get("response_format.type").String() != "json_object" || up.Get("reasoning_effort").String() != "high" {
		t.Errorf("upstream sent %s", got[0])
	}
	r := gjson.Parse(w.Body.String())
	if !r.Get("done").Bool() || r.Get("message.content").String() != "whole answer" || r.Get("message.thinking").String() != "pondered" ||
		r.Get("message.tool_calls.0.function.arguments.path").String() != "a.go" || r.Get("eval_count").Int() != 7 || strings.Count(w.Body.String(), "\n") > 1 {
		t.Errorf("whole reply %s", w.Body)
	}
	w = ollamaCall(t, h, "/api/chat", `{"model":"plan/seer","messages":[{"role":"user","content":"hi"}],"stream":false,"think":false}`)
	if r := gjson.Parse(w.Body.String()); w.Code != 200 || r.Get("message.thinking").Exists() || r.Get("message.content").String() != "whole answer" {
		t.Errorf("think false: %d %s", w.Code, w.Body)
	}
}

// /api/generate takes a prompt, a system prompt and images, and answers
// in "response", streamed or whole; the ollama CLI's name:latest for a
// model with no tag is the model.
func TestOllamaGenerate(t *testing.T) {
	fresh(t)
	var got []string
	ollamaPlan(t, ollamaUpstream(t, &got).URL+"/v1")
	h := localGuard(New().Handler())
	w := ollamaCall(t, h, "/api/generate", `{"model":"plan/seer:latest","prompt":"why is the sky blue","system":"be brief","images":["/9j/4AAQSkZJRgABAQ=="],"options":{"seed":7,"top_p":0.9,"num_predict":-1}}`)
	if w.Code != 200 || len(got) != 1 {
		t.Fatalf("generate: %d %s", w.Code, w.Body)
	}
	up := gjson.Parse(got[0])
	if up.Get("model").String() != "seer" || up.Get("messages.0.content").String() != "be brief" || up.Get("messages.1.content.0.text").String() != "why is the sky blue" ||
		!strings.HasPrefix(up.Get("messages.1.content.1.image_url.url").String(), "data:image/jpeg;base64,") || up.Get("seed").Int() != 7 || up.Get("top_p").Float() != 0.9 || up.Get("max_tokens").Exists() {
		t.Errorf("upstream sent %s", got[0])
	}
	lines := ndjson(t, w.Body.String())
	var text string
	for _, l := range lines {
		if l.Get("message").Exists() {
			t.Fatalf("generate answered with a message: %s", l.Raw)
		}
		text += l.Get("response").String()
	}
	if text != "Hello, world" || !lines[len(lines)-1].Get("done").Bool() {
		t.Errorf("generate streamed %s", w.Body)
	}
	w = ollamaCall(t, h, "/api/generate", `{"model":"plan/seer","prompt":"hi","stream":false}`)
	if r := gjson.Parse(w.Body.String()); w.Code != 200 || r.Get("response").String() != "whole answer" || !r.Get("done").Bool() {
		t.Errorf("generate whole: %d %s", w.Code, w.Body)
	}
	// nothing to say: the model loaded, as a client warms it up
	w = ollamaCall(t, h, "/api/generate", `{"model":"plan/seer"}`)
	if r := gjson.Parse(w.Body.String()); w.Code != 200 || r.Get("done_reason").String() != "load" || len(got) != 2 {
		t.Errorf("generate with no prompt: %d %s (upstream %d)", w.Code, w.Body, len(got))
	}
}

// A model uni-switch doesn't serve, and a provider's refusal, are told as
// Ollama tells an error: {"error": "…"} with the status.
func TestOllamaChatErrors(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"context too long for seer","type":"invalid_request_error"}}`)
	}))
	defer up.Close()
	ollamaPlan(t, up.URL+"/v1")
	h := localGuard(New().Handler())
	for _, path := range []string{"/api/chat", "/api/generate"} {
		w := ollamaCall(t, h, path, `{"model":"nowhere/none","messages":[{"role":"user","content":"hi"}],"prompt":"hi"}`)
		if e := gjson.Get(w.Body.String(), "error"); w.Code < 400 || e.Type != gjson.String || e.String() == "" {
			t.Errorf("%s, unknown model: %d %s", path, w.Code, w.Body)
		}
		w = ollamaCall(t, h, path, `{"model":"plan/seer","messages":[{"role":"user","content":"hi"}],"prompt":"hi"}`)
		if e := gjson.Get(w.Body.String(), "error"); w.Code != 400 || e.Type != gjson.String || !strings.Contains(e.String(), "context too long") {
			t.Errorf("%s, refused: %d %s", path, w.Code, w.Body)
		}
	}
	if w := ollamaCall(t, h, "/api/chat", `{"model":"plan/seer","messages":[{"role":"tool","content":"orphan"}]}`); w.Code != 400 || gjson.Get(w.Body.String(), "error").Type != gjson.String {
		t.Errorf("a tool result with no call: %d %s", w.Code, w.Body)
	}
}
