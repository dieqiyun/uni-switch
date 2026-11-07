package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiNormalizationFilesAndToolOwnership(t *testing.T) {
	body := Object{"model": "gemini-3-pro", "system_instruction": Object{"parts": []any{Object{"text": "rules"}}}, "contents": []any{Object{"role": "user", "parts": []any{Object{"file_data": Object{"mime_type": "application/pdf", "file_uri": "https://files.invalid/a.pdf"}}}}, Object{"role": "model", "parts": []any{Object{"function_call": Object{"name": "f", "args": Object{"my_key": 1}}, "thought_signature": "signed"}, Object{"function_response": Object{"name": "f", "response": Object{"my_key": 2}}}}}}, "tools": []any{Object{"function_declarations": []any{Object{"name": "f", "parameters": Object{"type": "OBJECT", "properties": Object{"my_key": Object{"type": "STRING", "nullable": true}}}}}}}, "generation_config": Object{"top_k": 8, "response_mime_type": "application/json"}}
	r := parseTest(t, Gemini, body)
	if r.Turns[0].Parts[0].Kind != "file" || len(r.Turns) != 3 || r.Turns[2].Role != "user" {
		t.Fatal(r.Turns)
	}
	if obj(r.Turns[1].Parts[0].Args)["my_key"] == nil || r.Turns[1].Parts[0].Signature != "signed" {
		t.Fatal(r)
	}
	built := buildTest(t, r, Gemini)
	if !strings.Contains(encode(built), "my_key") || field(built, "generationConfig", "topK") == nil || !strings.Contains(encode(built), "fileData") || !strings.Contains(encode(built), "thoughtSignature") {
		t.Fatal(encode(built))
	}
	for _, to := range []Protocol{Responses, Messages} {
		b := buildTest(t, r, to)
		if strings.Contains(encode(b), `"signature":"signed"`) {
			t.Fatal("foreign signature leaked", b)
		}
	}
}
func TestGeminiRequiredToolsAndOpaqueFields(t *testing.T) {
	body := Object{"model": "m", "contents": []any{Object{"parts": []any{Object{"text": "hello"}}}}, "tools": []any{Object{"functionDeclarations": []any{Object{"name": "yes", "parametersJsonSchema": Object{"properties": Object{"my_key": Object{"type": "string"}}}}}}}, "toolConfig": Object{"functionCallingConfig": Object{"mode": "ANY", "allowedFunctionNames": []any{"missing"}}}}
	if _, e := Parse(Gemini, body); e == nil {
		t.Fatal("required tools empty")
	}
	obj(field(body, "toolConfig", "functionCallingConfig"))["mode"] = "VALIDATED"
	r := parseTest(t, Gemini, body)
	if len(r.Tools) != 0 || r.Choice != "" {
		t.Fatal(r)
	}
	m := obj(geminiNormalize(Object{"generation_config": Object{"max_output_tokens": 1}, "generationConfig": Object{"maxOutputTokens": 2}, "labels": Object{"snake_case": "value"}, "function_call": Object{"args": Object{"snake_case": true}}}, false))
	if integer(field(m, "generationConfig", "maxOutputTokens")) != 2 || field(m, "labels", "snake_case") == nil || field(m, "functionCall", "args", "snake_case") == nil {
		t.Fatal(m)
	}
}
func TestGeminiStreamingNativeAndCodeAssist(t *testing.T) {
	chunk := func(parts []any, finish string) Object {
		c := Object{"content": Object{"role": "model", "parts": parts}}
		if finish != "" {
			c["finishReason"] = finish
		}
		return Object{"candidates": []any{c}}
	}
	chunks := []Object{chunk([]any{Object{"text": "中文"}}, ""), chunk([]any{Object{"text": "😀"}}, ""), chunk([]any{Object{"functionCall": Object{"name": "f", "args": Object{"some_key": true}}, "thoughtSignature": "signature"}}, "STOP"), {"usageMetadata": Object{"promptTokenCount": 8, "candidatesTokenCount": 2, "thoughtsTokenCount": 3, "totalTokenCount": 13}}}
	for _, wrapped := range []bool{false, true} {
		for _, to := range []Protocol{Gemini, Messages, Responses, Chat} {
			var input strings.Builder
			for _, c := range chunks {
				if wrapped {
					c = Object{"response": c}
				}
				input.WriteString(frame(c))
			}
			var output strings.Builder
			enc := NewEncoder(to, "m", nil, func(f Frame) error { output.WriteString(f.Data); return nil })
			_ = enc.Start()
			cache := cacheFor(Call{CacheDirectory: t.TempDir(), BaseURL: "https://test.invalid", APIKey: "synthetic"}, "m")
			if e := DecodeStream(Gemini, chopped{strings.NewReader(input.String()), 1}, cache.stream(to, Gemini, enc.Accept)); e != nil {
				t.Fatal(e)
			}
			got := output.String()
			if !strings.Contains(got, "中文") || !strings.Contains(got, "some_key") {
				t.Fatal(to, got)
			}
			if to == Messages && strings.Contains(got, `"signature":"signature"`) {
				t.Fatal("Gemini signature leaked into Messages")
			}
		}
	}
	if DecodeStream(Gemini, strings.NewReader(frame(chunks[0])), func(Event) error { return nil }) == nil {
		t.Fatal("accepted truncated Gemini stream")
	}
}
func TestGeminiSignatureScopeAndReplay(t *testing.T) {
	c := Call{CacheDirectory: t.TempDir(), BaseURL: "https://test.invalid", APIKey: "synthetic"}
	cache := cacheFor(c, "m")
	res := Result{Parts: []Part{{Kind: "call", ID: "call", Name: "f", Args: Object{}, Signature: strings.Repeat("signature", 1000), SealedBy: "gemini"}}}
	cache.renderResult(&res, Responses, Gemini)
	id := res.Parts[0].ID
	if len(id) > 100 || id == "call" {
		t.Fatal(id)
	}
	r := &Request{Turns: []Turn{{Role: "assistant", Parts: []Part{{Kind: "call", ID: id, Name: "f"}}}, {Role: "user", Parts: []Part{{Kind: "result", ID: id}}}}}
	cache.restore(r)
	if r.Turns[0].Parts[0].Signature == "" || r.Turns[0].Parts[0].ID != "call" || r.Turns[1].Parts[0].ID != "call" {
		t.Fatal(r)
	}
	r.Turns[0].Parts[0] = Part{Kind: "call", ID: id}
	c.APIKey = "different"
	cacheFor(c, "m").restore(r)
	if r.Turns[0].Parts[0].Signature != "" {
		t.Fatal("cross credential signature replay")
	}
}
func TestGeminiUnsignedHistoryAndSchemaFieldNames(t *testing.T) {
	r := &Request{Source: Chat, Model: "gemini-3", Turns: []Turn{{Role: "assistant", Parts: []Part{{Kind: "call", ID: "call", Name: "f", Args: Object{}}}}}}
	b := buildTest(t, r, Gemini)
	if field(obj(list(b["contents"])[0]), "parts") == nil || !strings.Contains(encode(b), "skip_thought_signature_validator") {
		t.Fatal(b)
	}
	schema := Object{"type": "OBJECT", "properties": Object{"propertyOrdering": Object{"type": "STRING"}, "nullable": Object{"type": "BOOLEAN"}, "type": Object{"enum": []any{"OBJECT", "ARRAY"}}}}
	normal := obj(geminiSchema(schema))
	props := obj(normal["properties"])
	if props["propertyOrdering"] == nil || props["nullable"] == nil || encode(obj(props["type"])["enum"]) != `["OBJECT","ARRAY"]` {
		t.Fatal(normal)
	}
}
func TestFourProtocolHTTPMatrix(t *testing.T) {
	for _, from := range []Protocol{Messages, Responses, Chat, Gemini} {
		for _, to := range []Protocol{Messages, Responses, Chat, Gemini} {
			for _, stream := range []bool{false, true} {
				t.Run(string(from)+"-"+string(to)+map[bool]string{false: "-json", true: "-sse"}[stream], func(t *testing.T) {
					up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if to == Gemini {
							if r.Header.Get("x-goog-api-key") != "synthetic-upstream" || !strings.Contains(r.URL.Path, "/models/m:") {
								t.Error(r.URL, r.Header)
							}
						} else if r.Header.Get("Authorization") != "Bearer synthetic-upstream" {
							t.Error(r.Header)
						}
						body, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(body), "hello") {
							t.Error(string(body))
						}
						result := Result{ID: "id", Stop: "stop", Parts: []Part{{Kind: "text", Text: "世界"}}, Usage: Object{"input_tokens": 2, "output_tokens": 1}}
						if !stream {
							w.Header().Set("Content-Type", "application/json")
							io.WriteString(w, encode(Render(result, to, "m", nil)))
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						enc := NewEncoder(to, "m", nil, func(f Frame) error { _, e := w.Write(frameBytes(f)); return e })
						_ = enc.Start()
						_ = enc.Accept(Event{Kind: "part", Index: 0, Part: result.Parts[0]})
						_ = enc.Accept(Event{Kind: "part_done", Index: 0, Part: result.Parts[0]})
						_ = enc.Accept(Event{Kind: "finish", Stop: "stop", Usage: result.Usage})
					}))
					defer up.Close()
					body := Object{"model": "m", "stream": stream}
					switch from {
					case Responses:
						body["input"] = "hello"
					case Chat, Messages:
						body["messages"] = []any{Object{"role": "user", "content": "hello"}}
					case Gemini:
						body["contents"] = []any{Object{"parts": []any{Object{"text": "hello"}}}}
					}
					token := strings.Repeat("a", 64)
					call := Call{Source: from, Upstream: to, BaseURL: up.URL + "/v1", APIKey: "synthetic-upstream", Token: token, Authorization: "Bearer " + token, Models: []Model{{ID: "m", Enabled: true}}, Body: body}
					status := 0
					var got strings.Builder
					e := NewService().Forward(context.Background(), call, func(p Packet) error {
						if p.Type == "head" {
							status = p.Status
						}
						if p.Type == "data" {
							b, _ := base64.StdEncoding.DecodeString(p.Data)
							got.Write(b)
						}
						return nil
					})
					if e != nil || status != 200 || !strings.Contains(got.String(), "世界") {
						t.Fatal(status, e, got.String())
					}
				})
			}
		}
	}
}
func TestSameProtocolRelayPreservesUnknownFields(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "caller_custom") || !strings.Contains(string(b), "new_vendor_field") {
			t.Error(string(b))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"new_vendor_output":true}`)
	}))
	defer up.Close()
	key := strings.Repeat("b", 64)
	c := Call{Source: Gemini, Upstream: Gemini, BaseURL: up.URL + "/v1beta", APIKey: "synthetic", Token: key, Authorization: "Bearer " + key, Models: []Model{{ID: "m", Enabled: true}}, Body: Object{"model": "m", "contents": []any{Object{"parts": []any{Object{"new_vendor_field": true}}}}, "caller_custom": true}}
	var got string
	if e := NewService().Forward(context.Background(), c, func(p Packet) error {
		if p.Type == "data" {
			b, _ := base64.StdEncoding.DecodeString(p.Data)
			got += string(b)
		}
		return nil
	}); e != nil || !strings.Contains(got, "new_vendor_output") {
		t.Fatal(e, got)
	}
}
func TestCodeAssistAccountEnvelopesAndForcedStream(t *testing.T) {
	for _, kind := range []string{"google", "antigravity"} {
		t.Run(kind, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != "/v1internal:streamGenerateContent?alt=sse" || r.Header.Get("Authorization") != "Bearer synthetic-account" || r.Header.Get("Accept") != "" {
					t.Error(r.URL, r.Header)
				}
				var body Object
				json.NewDecoder(r.Body).Decode(&body)
				if body["project"] != "test-project" || body["model"] != "m" || field(body, "request", "model") != nil || field(body, "request", "stream") != nil {
					t.Error(body)
				}
				if kind == "antigravity" {
					if body["requestType"] != "agent" || body["userAgent"] != "antigravity" || !strings.HasPrefix(str(field(body, "request", "sessionId")), "-") {
						t.Error(body)
					}
				} else if str(body["user_prompt_id"]) == "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "GeminiCLI/") {
					t.Error(body, r.Header)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, frame(Object{"response": Object{"candidates": []any{Object{"content": Object{"parts": []any{Object{"text": "account-ok"}}}, "finishReason": "STOP"}}}}))
			}))
			defer up.Close()
			token := strings.Repeat("a", 64)
			c := Call{Source: Responses, Upstream: Gemini, Transport: "codeassist", AccountKind: kind, Project: "test-project", BaseURL: up.URL, APIKey: "synthetic-account", Token: token, Authorization: "Bearer " + token, Models: []Model{{ID: "m", Enabled: true}}, Body: Object{"model": "m", "input": "hello"}}
			var got strings.Builder
			status := 0
			err := NewService().Forward(context.Background(), c, func(p Packet) error {
				if p.Type == "head" {
					status = p.Status
				}
				if p.Type == "data" {
					b, _ := base64.StdEncoding.DecodeString(p.Data)
					got.Write(b)
				}
				return nil
			})
			if err != nil || status != 200 || !strings.Contains(got.String(), "account-ok") {
				t.Fatal(err, status, got.String())
			}
		})
	}
}
func TestGeminiThinkingLevelsMatchGeneration(t *testing.T) {
	for _, tc := range []struct {
		model, effort, want string
		off                 bool
	}{
		{"gemini-3-pro", "medium", "high", false}, {"gemini-3-flash", "medium", "medium", false}, {"gemini-4-pro", "medium", "medium", false}, {"gemini-3-pro", "", "low", true}, {"gemini-3.1-flash-image", "", "high", true},
	} {
		r := &Request{Model: tc.model, Effort: tc.effort, Off: tc.off, Turns: []Turn{{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}}}}
		b, err := Build(r, Gemini, Model{ID: tc.model}, "")
		if err != nil || field(b, "generationConfig", "thinkingConfig", "thinkingLevel") != tc.want {
			t.Fatal(tc, b, err)
		}
	}
}
