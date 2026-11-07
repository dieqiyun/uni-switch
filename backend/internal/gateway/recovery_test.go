package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEndpointSDKPrefixesAndExplicitResources(t *testing.T) {
	for _, tt := range []struct {
		base string
		p    Protocol
		want string
	}{
		{"https://example.test", Responses, "https://example.test/v1/responses"},
		{"https://example.test/responses", Responses, "https://example.test/responses"},
		{"https://example.test/chat/completions/", Responses, "https://example.test/responses"},
		{"https://example.test/api/paas/v4", Chat, "https://example.test/api/paas/v4/chat/completions"},
		{"https://example.test", Messages, "https://example.test/v1/messages"},
		{"https://example.test/gateway/v1", Messages, "https://example.test/gateway/v1/messages"},
		{"https://example.test/gateway/messages/", Messages, "https://example.test/gateway/messages"},
		{"https://example.test/gateway/responses", Messages, "https://example.test/gateway/v1/messages"},
	} {
		got, e := endpoint(tt.base, tt.p)
		if e != nil || got != tt.want {
			t.Fatalf("%s %s: %s %v", tt.base, tt.p, got, e)
		}
	}
}

func TestHTTPFallbackRequiresEndpointEvidence(t *testing.T) {
	for _, tt := range []struct {
		status   int
		message  string
		fallback bool
	}{
		{404, "model not found", false}, {405, "method not allowed", false}, {401, "use /v1/chat/completions", false}, {429, "rate limited", false}, {500, "use /v1/chat/completions", false}, {400, "not supported in /v1/responses; use /v1/chat/completions", true},
	} {
		calls := 0
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path == "/v1/responses" {
				w.WriteHeader(tt.status)
				io.WriteString(w, encode(Object{"error": Object{"message": tt.message}}))
				return
			}
			if !tt.fallback {
				t.Error("unexpected replay", r.URL)
			}
			io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
		}))
		c := callTest(t, up.URL)
		c.Source = Messages
		c.Body = Object{"model": "synthetic-model", "messages": []any{Object{"role": "user", "content": "hi"}}}
		status, b := collect(t, NewService(), c)
		up.Close()
		want := 1
		if tt.fallback {
			want = 2
			if status != 200 || !bytes.Contains(b, []byte("ok")) {
				t.Fatal(status, string(b))
			}
		} else if status != tt.status {
			t.Fatal(status, tt)
		}
		if calls != want {
			t.Fatal(tt, calls)
		}
	}
}

func TestTerminalResponseCompletesMissingItemsAndOpaqueReasoning(t *testing.T) {
	items := []any{Object{"type": "reasoning", "id": "rs", "summary": []any{}, "encrypted_content": "opaque"}, Object{"type": "function_call", "call_id": "c", "name": "f", "arguments": "{\"x\":1}"}}
	sse := frame(Object{"type": "response.output_item.added", "output_index": 0, "item": Object{"type": "reasoning", "id": "rs"}}) + frame(Object{"type": "response.output_item.added", "output_index": 1, "item": Object{"type": "function_call", "call_id": "c", "name": "f"}}) + frame(Object{"type": "response.completed", "response": Object{"status": "completed", "output": items}})
	var parts []Part
	e := DecodeStream(Responses, strings.NewReader(sse), func(e Event) error {
		if e.Kind == "part_done" {
			parts = append(parts, e.Part)
		}
		return nil
	})
	if e != nil || len(parts) != 2 || parts[0].Raw["encrypted_content"] != "opaque" || integer(field(parts[1].Args, "x")) != 1 {
		t.Fatal(parts, e)
	}
	c := reasoningCache{directory: t.TempDir(), scope: "scope"}
	var id string
	e = DecodeStream(Responses, strings.NewReader(sse), c.stream(Messages, Responses, func(e Event) error {
		if e.Kind == "part" && e.Part.Kind == "call" {
			id = e.Part.ID
		}
		return nil
	}))
	if e != nil || !strings.Contains(id, carryMark) {
		t.Fatal(id, e)
	}
}

func TestCutReplyContinuesOnlyWithReliablePrefill(t *testing.T) {
	for _, withTool := range []bool{false, true} {
		calls := 0
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "text/event-stream")
			if calls == 1 {
				io.WriteString(w, frame(Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": ""}})+frame(Object{"type": "content_block_delta", "index": 0, "delta": Object{"type": "text_delta", "text": "first "}}))
				if withTool {
					io.WriteString(w, frame(Object{"type": "content_block_start", "index": 1, "content_block": Object{"type": "tool_use", "id": "c", "name": "f", "input": Object{}}}))
				}
				return
			}
			var body Object
			_ = decodeReader(r.Body, &body)
			if !strings.Contains(encode(body), `"text":"first"`) {
				t.Error(body)
			}
			io.WriteString(w, messageStream("first rest"))
		}))
		c := callTest(t, up.URL)
		c.Body["stream"] = true
		_, b := collect(t, NewService(), c)
		up.Close()
		if withTool {
			if calls != 1 || bytes.Contains(b, []byte("response.completed")) {
				t.Fatal(calls, string(b))
			}
			continue
		}
		if calls != 2 || !bytes.Contains(b, []byte("response.completed")) || bytes.Contains(b, []byte(`"delta":"first rest"`)) {
			t.Fatal(calls, string(b))
		}
	}
}

func TestChatPrefillModesAndReasoningReplay(t *testing.T) {
	for _, tt := range []struct{ base, model, want string }{
		{"https://api.deepseek.com", "deepseek-chat", "prefix"}, {"https://api.moonshot.cn/v1", "kimi-k2", "partial"}, {"http://127.0.0.1:1234/v1", "deepseek-r1", "prefix"}, {"https://relay.example/v1", "deepseek-r1", ""}, {"https://api.openai.com/v1", "gpt-5", ""},
	} {
		if got := prefillMode(tt.base, Chat, tt.model); got != tt.want {
			t.Fatal(tt, got)
		}
	}
	r := &Request{Source: Messages, Model: "deepseek-r1", Resume: true, Turns: []Turn{{"assistant", []Part{{Kind: "thinking", Text: "thought"}, {Kind: "text", Text: "prefix"}}}}}
	b, e := Build(r, Chat, Model{ID: r.Model}, "https://api.deepseek.com")
	if e != nil || field(list(b["messages"])[0], "reasoning_content") != "thought" || field(list(b["messages"])[0], "prefix") != true {
		t.Fatal(b, e)
	}
}

func TestOrdinaryChatCutNeverReplaysAndRecoveryStopsAfterTwo(t *testing.T) {
	for _, chat := range []bool{true, false} {
		calls := 0
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "text/event-stream")
			if chat {
				io.WriteString(w, frame(Object{"choices": []any{Object{"delta": Object{"content": "partial"}}}}))
			} else {
				io.WriteString(w, frame(Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": "partial"}}))
			}
		}))
		c := callTest(t, up.URL)
		c.Body["stream"] = true
		if chat {
			c.Source = Messages
			c.Body = Object{"model": "synthetic-model", "stream": true, "stop_sequences": []any{"stop"}, "messages": []any{Object{"role": "user", "content": "hi"}}}
		}
		_, b := collect(t, NewService(), c)
		up.Close()
		want := 3
		if chat {
			want = 1
		}
		if calls != want || bytes.Contains(b, []byte("response.completed")) || bytes.Contains(b, []byte("message_stop")) {
			t.Fatal(chat, calls, string(b))
		}
	}
}

func TestReasoningCacheReplaysWithinScopeOnly(t *testing.T) {
	c := reasoningCache{directory: t.TempDir(), scope: "same"}
	id := c.keep("c", []Object{{"type": "reasoning", "encrypted_content": "opaque"}})
	makeRequest := func() *Request {
		return &Request{Turns: []Turn{{"assistant", []Part{{Kind: "call", ID: id}}}, {"user", []Part{{Kind: "result", ID: id}}}}}
	}
	r := makeRequest()
	c.restore(r)
	if len(r.Turns[0].Parts) != 2 || r.Turns[0].Parts[0].Raw["encrypted_content"] != "opaque" || r.Turns[1].Parts[0].ID != "c" {
		t.Fatal(r)
	}
	r = makeRequest()
	other := c
	other.scope = "other"
	other.restore(r)
	if !reflect.DeepEqual(r.Turns[0].Parts, []Part{{Kind: "call", ID: "c"}}) {
		t.Fatal(r)
	}
}
