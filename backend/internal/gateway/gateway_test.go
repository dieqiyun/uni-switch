package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

// Behaviour references: yetone/magpie 665b9aff, namespace_test,
// thinking_signature_test, responses_system_turn_test, translate_stream_test,
// tool_use_ids, prefill_test and provider/test.tinyBody. These are synthetic
// assertions of reviewed rules; no upstream package is a test dependency.
func parseTest(t *testing.T, p Protocol, body Object) *Request {
	t.Helper()
	r, e := Parse(p, body)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func buildTest(t *testing.T, r *Request, to Protocol) Object {
	t.Helper()
	v, e := Build(r, to, Model{ID: r.Model}, "https://example.test/v1")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func bodyTest() Object      { return Object{"model": "synthetic-model", "input": "hello"} }
func frame(v Object) string { return "data: " + encode(v) + "\n\n" }
func messageStream(text string) string {
	return frame(Object{"type": "message_start", "message": Object{"usage": Object{"input_tokens": 12, "cache_read_input_tokens": 3}}}) + frame(Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": ""}}) + frame(Object{"type": "content_block_delta", "index": 0, "delta": Object{"type": "text_delta", "text": text}}) + frame(Object{"type": "content_block_stop", "index": 0}) + frame(Object{"type": "message_delta", "delta": Object{"stop_reason": "end_turn"}, "usage": Object{"output_tokens": 2}}) + frame(Object{"type": "message_stop"})
}

type chopped struct {
	r io.Reader
	n int
}

func (c chopped) Read(b []byte) (int, error) { return c.r.Read(b[:min(len(b), c.n)]) }
func TestFramesCutAtEveryByteAndNewline(t *testing.T) {
	for _, nl := range []string{"\n", "\r", "\r\n"} {
		for n := 1; n < 40; n++ {
			in := "\xef\xbb\xbf: ping" + nl + "event: hello" + nl + "data: 中文" + nl + "data: 😀" + nl + nl + "data: tail"
			var got []Frame
			e := ReadSSE(chopped{strings.NewReader(in), n}, func(f Frame) error { got = append(got, f); return nil })
			want := []Frame{{"hello", "中文\n😀"}, {"", "tail"}}
			if e != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("nl=%q n=%d: %#v %v", nl, n, got, e)
			}
		}
	}
}
func TestFramesLimitsAndInvalidUTF8(t *testing.T) {
	for _, s := range []string{"data: \xff\n\n", "data: " + strings.Repeat("a", 4<<20) + "\n\n"} {
		if ReadSSE(strings.NewReader(s), func(Frame) error { return nil }) == nil {
			t.Fatal("accepted invalid frame")
		}
	}
}
func TestThreeStreamOutputsAndTruncation(t *testing.T) {
	for _, to := range []Protocol{Responses, Messages, Chat} {
		for n := 1; n < 24; n++ {
			var output []Frame
			enc := NewEncoder(to, "client-model", nil, func(f Frame) error { output = append(output, f); return nil })
			if e := enc.Start(); e != nil {
				t.Fatal(e)
			}
			if e := DecodeStream(Messages, chopped{strings.NewReader(messageStream("中文😀")), n}, enc.Accept); e != nil {
				t.Fatal(e)
			}
			joined := ""
			for _, f := range output {
				joined += f.Data
			}
			if !strings.Contains(joined, "中文😀") {
				t.Fatal(joined)
			}
			if to == Responses && !strings.Contains(joined, "response.completed") {
				t.Fatal("missing completion")
			}
		}
	}
	for _, s := range []string{messageStream("a")[:200], frame(Object{"type": "content_block_start", "content_block": Object{"type": "text", "text": ""}}) + frame(Object{"type": "message_stop"})} {
		if DecodeStream(Messages, strings.NewReader(s), func(Event) error { return nil }) == nil {
			t.Fatal("truncated stream succeeded")
		}
	}
}
func TestCompletionStopsReadingWithoutEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- DecodeStream(Messages, reader, func(Event) error { return nil }) }()
	if _, e := io.WriteString(writer, messageStream("ok")); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("waited for EOF after final event")
	}
}
func TestToolIDsAreReversibleAndPrefixSafe(t *testing.T) {
	for _, id := range []string{"call_ok", "unsafe:中文|name", "us_already", "😀"} {
		safe := safeID(id)
		if !safeChars(safe) || rawID(safe) != id {
			t.Fatalf("%s -> %s", id, safe)
		}
	}
}
func TestLateInstructionsStayAtTheirPosition(t *testing.T) {
	r := parseTest(t, Responses, Object{"model": "m", "instructions": "initial", "input": []any{Object{"role": "developer", "content": "first"}, Object{"role": "user", "content": "ask"}, Object{"role": "assistant", "content": "said"}, Object{"role": "system", "content": "later"}, Object{"role": "user", "content": "next"}}})
	v := buildTest(t, r, Messages)
	if !strings.Contains(str(field(list(v["system"])[0], "text")), "initial\n\nfirst") {
		t.Fatal(v)
	}
	ms := list(v["messages"])
	if len(ms) != 3 || !strings.Contains(str(field(list(field(ms[2], "content"))[0], "text")), "<system-reminder>\nlater\n</system-reminder>") {
		t.Fatal(encode(v))
	}
}
func TestNamespaceCustomToolsAndHistory(t *testing.T) {
	r := parseTest(t, Responses, Object{"model": "m", "tools": []any{Object{"type": "namespace", "name": "shell", "tools": []any{Object{"type": "custom", "name": "apply_patch"}}}}, "input": []any{Object{"type": "custom_tool_call", "namespace": "shell", "name": "apply_patch", "call_id": "c:1", "input": "  patch\n中文"}, Object{"type": "custom_tool_call_output", "call_id": "c:1", "output": "done"}}})
	v := buildTest(t, r, Messages)
	if field(list(v["tools"])[0], "name") != "shell__apply_patch" {
		t.Fatal(v)
	}
	call := obj(list(field(list(v["messages"])[0], "content"))[0])
	if field(call, "input", "input") != "  patch\n中文" {
		t.Fatal(call)
	}
	response := Render(Result{Stop: "tool", Parts: []Part{{Kind: "call", ID: "c1", Name: r.Tools[0].Name, Args: Object{"input": "  patch\n中文"}}}}, Responses, "m", r.Tools)
	item := obj(list(response["output"])[0])
	if item["namespace"] != "shell" || item["input"] != "  patch\n中文" || item["type"] != "custom_tool_call" {
		t.Fatal(item)
	}
}
func TestLongToolNamesAreStable(t *testing.T) {
	body := bodyTest()
	body["tools"] = []any{Object{"type": "function", "name": strings.Repeat("long_name_", 20)}}
	a := parseTest(t, Responses, body)
	b := parseTest(t, Responses, body)
	if a.Tools[0].Name != b.Tools[0].Name || len(a.Tools[0].Name) > 64 {
		t.Fatal("unstable name")
	}
}
func TestPairingReportsMissingAndOrphansWithoutSuccess(t *testing.T) {
	r := &Request{Turns: []Turn{{"assistant", []Part{{Kind: "call", ID: "a", Name: "x"}, {Kind: "call", ID: "b", Name: "x"}}}, {"user", []Part{{Kind: "result", ID: "a", Text: "one"}, {Kind: "result", ID: "a", Text: "duplicate"}, {Kind: "result", ID: "orphan", Text: "orphan data"}}}}}
	r.pairTools()
	ps := r.Turns[1].Parts
	if len(ps) != 3 || ps[0].Text != "one" || ps[1].ID != "b" || !ps[1].IsError || ps[2].Text != "orphan data" {
		t.Fatalf("%#v", ps)
	}
}
func TestEffortBudgetRulesAndStandaloneEffort(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{{4096, "low"}, {10000, "medium"}, {12000, "medium"}, {24000, "high"}, {32000, "xhigh"}} {
		if effortBudget(tc.n) != tc.want {
			t.Fatal(tc)
		}
	}
	r := parseTest(t, Messages, Object{"model": "claude-sonnet-4-6", "messages": []any{Object{"role": "user", "content": "title"}}, "output_config": Object{"effort": "low"}})
	v := buildTest(t, r, Responses)
	if v["reasoning"] != nil {
		t.Fatal("standalone title effort enabled thinking", v)
	}
	r.Think = true
	r.Effort = "high"
	v = buildTest(t, r, Messages)
	if field(v, "thinking", "type") != "adaptive" || field(v, "output_config", "effort") != "high" {
		t.Fatal(v)
	}
}
func TestSignedThinkingSurvivesRoundTripWithEmptyText(t *testing.T) {
	result := Result{Stop: "tool", Parts: []Part{{Kind: "thinking", Signature: "only-signature", SealedBy: "messages", Raw: Object{"type": "thinking", "thinking": "", "signature": ""}}, {Kind: "call", ID: "id", Name: "f", Args: Object{}}}}
	response := Render(result, Responses, "m", nil)
	r := parseTest(t, Responses, Object{"model": "m", "input": response["output"]})
	v := buildTest(t, r, Messages)
	p := obj(list(field(list(v["messages"])[0], "content"))[0])
	if p["signature"] != "only-signature" {
		t.Fatal(v)
	}
	other := buildTest(t, r, Responses)
	if strings.Contains(encode(other), "only-signature") {
		t.Fatal("Anthropic seal leaked to OpenAI")
	}
}
func TestReasoningCarriedAcrossToolsAndCredentialScoped(t *testing.T) {
	c := cacheFor(Call{CacheDirectory: t.TempDir(), BaseURL: "https://example.test", APIKey: "synthetic"}, "m")
	result := Result{Parts: []Part{{Kind: "thinking", SealedBy: "responses", Raw: Object{"type": "reasoning", "encrypted_content": "opaque"}}, {Kind: "call", ID: "original", Name: "f", Args: Object{}}}}
	c.renderResult(&result, Messages, Responses)
	id := result.Parts[1].ID
	if id == "original" {
		t.Fatal("no carried key")
	}
	r := &Request{Turns: []Turn{{"assistant", []Part{{Kind: "call", ID: id}}}, {"user", []Part{{Kind: "result", ID: id}}}}}
	c.restore(r)
	if r.Turns[0].Parts[0].Raw["encrypted_content"] != "opaque" || r.Turns[0].Parts[1].ID != "original" || r.Turns[1].Parts[0].ID != "original" {
		t.Fatal(r)
	}
	other := c
	other.scope = "different-account"
	r = &Request{Turns: []Turn{{"assistant", []Part{{Kind: "call", ID: id}}}}}
	other.restore(r)
	if len(r.Turns[0].Parts) != 1 || r.Turns[0].Parts[0].ID != "original" {
		t.Fatal("foreign sealed reasoning restored")
	}
}
func TestCustomStreamEscapesCutEveryByte(t *testing.T) {
	for _, raw := range []string{`{"input":"a\n\u4e2d\u6587\ud83d\ude00\\\"end"}`, `"a\n\u4e2d\u6587\ud83d\ude00\\\"end"`, `{"other":{"input":"wrong"},"input":"a\n中文😀\\\"end"}`} {
		for n := 1; n < 18; n++ {
			c := customReader{}
			got := ""
			for i := 0; i < len(raw); i += n {
				got += c.feed(raw[i:min(i+n, len(raw))])
			}
			if got != "a\n中文😀\\\"end" {
				t.Fatalf("n=%d got=%q raw=%s", n, got, raw)
			}
		}
	}
}
func TestCustomOutputStreamsBeforeCallEnd(t *testing.T) {
	tools := []Tool{{Name: "wrapped", Original: "apply_patch", Custom: true}}
	var fs []Frame
	e := NewEncoder(Responses, "m", tools, func(f Frame) error { fs = append(fs, f); return nil })
	_ = e.Start()
	_ = e.Accept(Event{Kind: "part", Index: 0, Part: Part{Kind: "call", ID: "id", Name: "wrapped"}})
	_ = e.Accept(Event{Kind: "delta", Index: 0, Field: "arguments", Text: `{"input":"first`})
	if !strings.Contains(encode(fs), "first") {
		t.Fatal("custom input was buffered until end")
	}
	_ = e.Accept(Event{Kind: "delta", Index: 0, Field: "arguments", Text: ` second"}`})
	if err := e.Accept(Event{Kind: "part_done", Index: 0, Part: Part{Kind: "call", ID: "id", Name: "wrapped", Args: Object{"input": "first second"}}}); err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, f := range fs {
		var v Object
		_ = decode([]byte(f.Data), &v)
		if v["type"] == "response.custom_tool_call_input.delta" {
			text += str(v["delta"])
		}
	}
	if text != "first second" {
		t.Fatal(text)
	}
}
func TestStructuredFormatAndChatHistory(t *testing.T) {
	r := &Request{Source: Messages, Model: "m", Turns: []Turn{{"assistant", []Part{{Kind: "text", Text: "look"}, {Kind: "call", ID: "c", Name: "f", Args: Object{}}}}, {"user", []Part{{Kind: "result", ID: "c", Text: "ok"}}}}, Format: Object{"type": "json_schema", "schema": Object{"type": "object"}}}
	v := buildTest(t, r, Chat)
	ms := list(v["messages"])
	if len(ms) != 2 || field(ms[0], "role") != "assistant" || len(list(field(ms[0], "tool_calls"))) != 1 || field(v, "response_format", "json_schema", "schema", "type") != "object" {
		t.Fatal(v)
	}
}
func TestEndpointFalseUsesChatCompletionsKey(t *testing.T) {
	r := parseTest(t, Responses, bodyTest())
	_, e := Build(r, Chat, Model{ID: r.Model, Metadata: &modelmeta.Resolved{Profile: Object{"endpoints": Object{"chatCompletions": false}}}}, "")
	if e == nil {
		t.Fatal("explicit false ignored")
	}
}
func TestDesktopAliasesKnownFNVAndLevels(t *testing.T) {
	m := Model{ID: "gpt-5.4", Metadata: &modelmeta.Resolved{Profile: Object{"reasoningEfforts": []string{"low", "high"}}}}
	if !strings.HasPrefix(DesktopID(m), "mythos-uni-switch-") {
		t.Fatal(DesktopID(m))
	}
	m.ID = "anthropic/claude-opus-4.6-20260205"
	if !strings.HasSuffix(DesktopID(m), ".anthropic.claude-opus-4-6") {
		t.Fatal(DesktopID(m))
	}
	c := Call{DefaultModel: "gpt-5.4", Models: []Model{{ID: "gpt-5.4", Enabled: true}, {ID: "disabled", Enabled: false}}}
	if _, e := c.resolveModel("disabled"); e == nil {
		t.Fatal("disabled fallback")
	}
	if _, e := c.resolveModel("claude-sonnet-4-6-uni-stale"); e == nil {
		t.Fatal("stale alias fallback")
	}
}
func callTest(t *testing.T, base string) Call {
	t.Helper()
	return Call{Source: Responses, BaseURL: base, APIKey: "synthetic-key", AuthMode: "x-api-key", Token: strings.Repeat("a", 64), Authorization: "Bearer " + strings.Repeat("a", 64), DefaultModel: "synthetic-model", Body: bodyTest(), CacheDirectory: t.TempDir()}
}
func collect(t *testing.T, s *Service, c Call) (int, []byte) {
	t.Helper()
	status := 0
	var b []byte
	e := s.Forward(context.Background(), c, func(p Packet) error {
		if p.Type == "head" {
			status = p.Status
		}
		if p.Type == "data" {
			v, e := base64.StdEncoding.DecodeString(p.Data)
			if e != nil {
				return e
			}
			b = append(b, v...)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return status, b
}
func TestCountTokensForChatOnlyModelDoesNotSendInference(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("local token estimate must not contact the vendor")
	}))
	defer up.Close()
	c := callTest(t, up.URL)
	c.Source = Messages
	c.Operation = "count_tokens"
	c.Body = Object{"model": "synthetic-model", "system": "1234", "messages": []any{Object{"role": "user", "content": "12345678"}}}
	c.Models = []Model{{ID: "synthetic-model", Enabled: true, Metadata: &modelmeta.Resolved{Profile: Object{"endpoints": Object{"responses": false, "chatCompletions": true}}}}}
	status, body := collect(t, NewService(), c)
	if status != 200 || string(body) != `{"input_tokens":3}` {
		t.Fatal(status, string(body))
	}
}
func TestHTTPGatewayAuthRoutingAndSecretRedaction(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/prefix/v1/messages" || r.Header.Get("x-api-key") != "synthetic-key" || r.Header.Get("anthropic-version") == "" {
			t.Error(r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))
	defer up.Close()
	c := callTest(t, up.URL+"/prefix")
	status, b := collect(t, NewService(), c)
	if status != 200 || !bytes.Contains(b, []byte(`"text":"ok"`)) {
		t.Fatal(status, string(b))
	}
	c.HasOrigin = true
	status, _ = collect(t, NewService(), c)
	if status != 401 || calls != 1 {
		t.Fatal(status, calls)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"synthetic-key rejected"}}`)
	}))
	defer bad.Close()
	c = callTest(t, bad.URL)
	status, b = collect(t, NewService(), c)
	if status != 401 || bytes.Contains(b, []byte("synthetic-key")) {
		t.Fatal(status, string(b))
	}
}
func TestHTTPStreamCutNeverCompletesAndCancellation(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, frame(Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": ""}})+frame(Object{"type": "content_block_delta", "index": 0, "delta": Object{"type": "text_delta", "text": "partial"}}))
	}))
	defer up.Close()
	c := callTest(t, up.URL)
	c.Body["stream"] = true
	status, b := collect(t, NewService(), c)
	if status != 200 || !bytes.Contains(b, []byte(`"type":"error"`)) || bytes.Contains(b, []byte("response.completed")) {
		t.Fatal(status, string(b))
	}
	cancelSeen := make(chan struct{})
	waiting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelSeen)
	}))
	defer waiting.Close()
	c = callTest(t, waiting.URL)
	c.Body["stream"] = true
	ctx, cancel := context.WithCancel(context.Background())
	e := NewService().Forward(ctx, c, func(p Packet) error {
		if p.Type == "head" {
			cancel()
		}
		return nil
	})
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	select {
	case <-cancelSeen:
	case <-time.After(time.Second):
		t.Fatal("owned upstream not cancelled")
	}
}
func TestPrefillRetryIsBoundedAndOnlyRecognised(t *testing.T) {
	for _, recognised := range []bool{false, true} {
		count := 0
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			if count == 1 {
				w.WriteHeader(400)
				msg := "bad request"
				if recognised {
					msg = "does not support prefill"
				}
				io.WriteString(w, encode(Object{"error": Object{"message": msg}}))
				return
			}
			var body Object
			_ = decodeReader(r.Body, &body)
			if !strings.Contains(encode(body), "Continue.") {
				t.Error(body)
			}
			io.WriteString(w, `{"content":[{"type":"text","text":"continued"}],"stop_reason":"end_turn"}`)
		}))
		c := callTest(t, up.URL)
		c.Body["input"] = []any{Object{"role": "assistant", "content": "partial"}}
		status, _ := collect(t, NewService(), c)
		up.Close()
		if recognised && (count != 2 || status != 200) || !recognised && (count != 1 || status != 400) {
			t.Fatal(recognised, count, status)
		}
	}
}
func decodeReader(r io.Reader, v any) error {
	b, e := io.ReadAll(r)
	if e != nil {
		return e
	}
	return decode(b, v)
}
func TestTinyConnectionProbeDoesNotProbeCapabilities(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var v Object
		if decodeReader(r.Body, &v) != nil {
			t.Fatal("invalid body")
		}
		if strings.Contains(encode(v), "tools") || strings.Contains(encode(v), "image") || v["stream"] == true {
			t.Error(v)
		}
		if n := max(integer(v["max_tokens"]), integer(v["max_output_tokens"])); n != 16 {
			t.Error(v)
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer up.Close()
	for _, endpoint := range []string{"messages", "responses", "chat_completions"} {
		o, e := NewService().TestConnection(context.Background(), Probe{BaseURL: up.URL, APIKey: "synthetic", Model: "m", Endpoint: endpoint, Consent: true})
		if e != nil || o.State != "available" {
			t.Fatal(o, e)
		}
	}
	_, e := NewService().TestConnection(context.Background(), Probe{BaseURL: up.URL, APIKey: "synthetic", Model: "m", Endpoint: "responses"})
	if e == nil || calls != 3 {
		t.Fatal("consent did not gate network")
	}
}
func TestCompactionTriggerThenResume(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v Object
		_ = decodeReader(r.Body, &v)
		if !strings.Contains(encode(v), compactPrompt) || field(v, "tool_choice", "type") != "none" {
			t.Error(v)
		}
		io.WriteString(w, `{"content":[{"type":"text","text":"summary with next step"}],"stop_reason":"end_turn"}`)
	}))
	defer up.Close()
	c := callTest(t, up.URL)
	c.Body["input"] = []any{Object{"type": "function_call", "call_id": "c", "name": "f", "arguments": "{}"}, Object{"type": "compaction_trigger"}}
	status, b := collect(t, NewService(), c)
	if status != 200 {
		t.Fatal(string(b))
	}
	var v Object
	_ = decode(b, &v)
	r := parseTest(t, Responses, Object{"model": "m", "input": v["output"]})
	if !strings.Contains(encode(buildTest(t, r, Messages)), "summary with next step") {
		t.Fatal(r)
	}
	c.Operation = "compact"
	status, _ = collect(t, NewService(), c)
	if status != 400 {
		t.Fatal("legacy compact endpoint still active")
	}
}
