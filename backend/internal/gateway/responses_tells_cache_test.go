package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The usage bodies sparrowbits got from ModelScope's API-Inference for the
// same ~12K-token prompt asked twice (#1578): Responses says 12288 of it
// was read from the cache, Chat says nothing of the cache at all.
const (
	modelScopeResponsesUsage = `{"input_tokens": 12396, "input_tokens_details": {"cached_tokens": 12288}, "output_tokens": 32, "total_tokens": 12428}`
	modelScopeChatUsage      = `{"prompt_tokens": 12364, "completion_tokens": 293, "total_tokens": 12657}`
)

// fakeModelScope answers on Chat and Responses with the reporter's usage,
// streamed when asked, and records each request's path. A model in
// notOnResponses is answered 404 on Responses.
func fakeModelScope(t *testing.T, chatUsage string, notOnResponses ...string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		var body struct{ Model string }
		json.Unmarshal(b, &body)
		stream := streamOf(b)
		switch {
		case r.URL.Path == "/v1/responses" && strings.Contains(strings.Join(notOnResponses, "\x00"), body.Model):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"message":"Not Found"}}`)
		case r.URL.Path == "/v1/responses" && stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"i\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n"+
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"x\",\"output\":[{\"type\":\"message\",\"id\":\"i\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":"+modelScopeResponsesUsage+"}}\n\n")
		case r.URL.Path == "/v1/responses":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"r","object":"response","status":"completed","model":"x","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":`+modelScopeResponsesUsage+`}`)
		case r.URL.Path == "/v1/chat/completions" && stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n"+
				"data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":"+chatUsage+"}\n\ndata: [DONE]\n\n")
		case r.URL.Path == "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"c","object":"chat.completion","model":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":`+chatUsage+`}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		p := paths
		paths = nil
		return p
	}
}

// messagesUsage is the usage a Messages reply gave the client, whole or
// streamed: the last value of each field the stream named.
func messagesUsage(t *testing.T, reply string) map[string]int {
	t.Helper()
	out := map[string]int{}
	take := func(u map[string]any) {
		for k, v := range u {
			if n, ok := v.(float64); ok {
				out[k] = int(n)
			}
		}
	}
	var whole struct {
		Usage map[string]any `json:"usage"`
	}
	if json.Unmarshal([]byte(reply), &whole) == nil && whole.Usage != nil {
		take(whole.Usage)
		return out
	}
	for _, line := range strings.Split(reply, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var ev struct {
			Usage   map[string]any `json:"usage"`
			Message struct {
				Usage map[string]any `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		take(ev.Message.Usage)
		take(ev.Usage)
	}
	return out
}

// Claude Desktop's Messages to a ModelScope model are asked on ModelScope's
// Responses API, which tells what its cache answered, not on Chat, which
// doesn't (#1578, sparrowbits: 0 of 34.9M tokens read from cache over 202
// requests on Chat, where Codex on Responses read 94%). The client is told
// the 12288 tokens read from cache, streamed and whole.
func TestModelScopeMessagesTellTheCache(t *testing.T) {
	srv, seen := fakeModelScope(t, modelScopeChatUsage)
	for _, stream := range []string{"true", "false"} {
		fresh(t)
		if err := provider.Save(provider.Provider{ID: "modelscope", Preset: "modelscope", Name: "ModelScope", Key: "k",
			Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Models: []string{"Qwen/Qwen3.8-Flash-Next"}}); err != nil {
			t.Fatal(err)
		}
		seen()
		code, reply := post(t, "/v1/messages", `{"model":"modelscope/Qwen/Qwen3.8-Flash-Next","max_tokens":100,"stream":`+stream+
			`,"system":[{"type":"text","text":"you are","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 {
			t.Fatalf("%d %s", code, reply)
		}
		if paths := seen(); len(paths) != 1 || paths[0] != "/v1/responses" {
			t.Fatalf("stream=%s: Messages went to %v, want /v1/responses", stream, paths)
		}
		if u := messagesUsage(t, reply); u["cache_read_input_tokens"] != 12288 || u["input_tokens"] != 108 || u["output_tokens"] != 32 {
			t.Errorf("stream=%s: usage %v, want 12288 read from cache, 108 input, 32 output\n%s", stream, u, reply)
		}
	}
}

// A model ModelScope doesn't serve on Responses (404 there) is asked on
// Chat, as before, and stays there.
func TestModelScopeModelNotOnResponsesGoesToChat(t *testing.T) {
	srv, seen := fakeModelScope(t, modelScopeChatUsage, "Qwen/Qwen-Chat-Only")
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "modelscope", Preset: "modelscope", Name: "ModelScope", Key: "k",
		Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Models: []string{"Qwen/Qwen-Chat-Only"}}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	ask := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
			`{"model":"modelscope/Qwen/Qwen-Chat-Only","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ok") {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	}
	seen()
	ask()
	if paths := seen(); len(paths) != 2 || paths[0] != "/v1/responses" || paths[1] != "/v1/chat/completions" {
		t.Fatalf("went to %v, want Responses then Chat", paths)
	}
	ask()
	if paths := seen(); len(paths) != 1 || paths[0] != "/v1/chat/completions" {
		t.Fatalf("second request went to %v, want Chat alone", paths)
	}
}

// A provider with Chat and Responses that isn't ModelScope keeps Chat for a
// translated request (nothing is known of its Responses API), and a Chat
// API that tells its cache in prompt_tokens_details.cached_tokens reaches a
// Messages client as cache_read_input_tokens.
func TestOtherChatAndResponsesKeepChat(t *testing.T) {
	srv, seen := fakeModelScope(t, `{"prompt_tokens": 12364, "completion_tokens": 293, "total_tokens": 12657, "prompt_tokens_details": {"cached_tokens": 12288}}`)
	for _, stream := range []string{"true", "false"} {
		fresh(t)
		if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k",
			Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Models: []string{"glm-5"}}); err != nil {
			t.Fatal(err)
		}
		seen()
		code, reply := post(t, "/v1/messages", `{"model":"relay/glm-5","max_tokens":100,"stream":`+stream+`,"messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 {
			t.Fatalf("%d %s", code, reply)
		}
		if paths := seen(); len(paths) != 1 || paths[0] != "/v1/chat/completions" {
			t.Fatalf("stream=%s: went to %v, want Chat", stream, paths)
		}
		if u := messagesUsage(t, reply); u["cache_read_input_tokens"] != 12288 || u["input_tokens"] != 76 {
			t.Errorf("stream=%s: usage %v, want 12288 read from cache, 76 input", stream, u)
		}
	}
}

// Agents that ask each model on one API (Pi, omp, droid) are wired to
// ModelScope's Responses, by its preset or its host; another vendor with
// both keeps Chat.
func TestModelScopeNativeIsResponses(t *testing.T) {
	for _, c := range []struct {
		p    provider.Provider
		want provider.Protocol
	}{
		{provider.Provider{Preset: "modelscope", Chat: "http://127.0.0.1:1/v1", Responses: "http://127.0.0.1:1/v1"}, provider.Responses},
		{provider.Provider{Chat: "https://api-inference.modelscope.cn/v1", Responses: "https://api-inference.modelscope.cn/v1"}, provider.Responses},
		{provider.Provider{Chat: "https://api-inference.modelscope.cn/v1"}, provider.Chat},
		{provider.Provider{Preset: "groq", Chat: "https://api.groq.com/openai/v1", Responses: "https://api.groq.com/openai/v1"}, provider.Chat},
	} {
		if got := c.p.Native("Qwen/Qwen3.8-Flash-Next"); got != c.want {
			t.Errorf("preset %q, chat %s, responses %s: Native %q, want %q", c.p.Preset, c.p.Chat, c.p.Responses, got, c.want)
		}
	}
}
