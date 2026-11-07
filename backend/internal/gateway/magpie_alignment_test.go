package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

// The cases in this file are behavioral contracts reviewed against the pinned
// Magpie files recorded in docs/magpie-core-behavior-reference.json.
func TestReferenceChatHistoryMissingIDsAreStableAndPaired(t *testing.T) {
	body := Object{"model": "m", "messages": []any{
		Object{"role": "user", "content": "run both"},
		Object{"role": "assistant", "tool_calls": []any{
			Object{"function": Object{"name": "one", "arguments": `{ "x": 1 }`}},
			Object{"id": nil, "function": Object{"name": "two", "arguments": `{}`}},
		}},
		Object{"role": "tool", "content": "first"}, Object{"role": "tool", "content": "second"},
	}}
	before := encode(body)
	first := parseTest(t, Chat, body)
	second := parseTest(t, Chat, body)
	if !reflect.DeepEqual(first.Turns, second.Turns) || encode(body) != before {
		t.Fatal("history IDs must be stable without changing the caller's body")
	}
	calls, results := first.Turns[1].Parts, first.Turns[2].Parts
	if calls[0].ID == "" || calls[0].ID == calls[1].ID || results[0].ID != calls[0].ID || results[1].ID != calls[1].ID {
		t.Fatalf("calls %#v results %#v", calls, results)
	}
}

func TestReferenceChatStreamMintsIDsAndSupportsReusedIndices(t *testing.T) {
	chunk := func(id, name, args string) string {
		return frame(Object{"choices": []any{Object{"index": 0, "delta": Object{"tool_calls": []any{Object{"index": 0, "id": id, "function": Object{"name": name, "arguments": args}}}}}}})
	}
	for _, missing := range []bool{false, true} {
		id := "a"
		if missing {
			id = ""
		}
		input := chunk(id, "first", `{ "x":`) + chunk(id, "first", ` 1 }`) + chunk("b", "second", `{}`) + frame(Object{"choices": []any{Object{"index": 0, "finish_reason": "tool_calls"}}})
		var calls []Part
		err := DecodeStream(Chat, strings.NewReader(input), func(e Event) error {
			if e.Kind == "part_done" && e.Part.Kind == "call" {
				calls = append(calls, e.Part)
			}
			return nil
		})
		if err != nil || len(calls) != 2 || calls[0].ID == "" || calls[1].ID != "b" || calls[0].Name != "first" || rawArguments(calls[0].Args) != `{ "x": 1 }` {
			t.Fatalf("missing=%v calls=%#v err=%v", missing, calls, err)
		}
	}
}

func TestReferenceBareStreamErrorsAreNotTransportCuts(t *testing.T) {
	for _, p := range []Protocol{Messages, Responses, Chat, Gemini} {
		err := DecodeStream(p, strings.NewReader(": keepalive\n{\"error\":{\"message\":\"synthetic refusal\"}}\n"), func(Event) error { return nil })
		if err == nil || errors.Is(err, errTruncated) || resumable(err) {
			t.Fatalf("%s must recognize the upstream error, got %v", p, err)
		}
	}
}

func TestReferenceCustomResponsesInputStreamsAsWrappedArguments(t *testing.T) {
	input := frame(Object{"type": "response.output_item.added", "output_index": 0, "item": Object{"type": "custom_tool_call", "call_id": "c", "name": "apply_patch"}})
	for _, delta := range []string{"*** Begin", " Patch\n+\"中文\"\\x\n"} {
		input += frame(Object{"type": "response.custom_tool_call_input.delta", "output_index": 0, "delta": delta})
	}
	whole := "*** Begin Patch\n+\"中文\"\\x\n"
	input += frame(Object{"type": "response.output_item.done", "output_index": 0, "item": Object{"type": "custom_tool_call", "call_id": "c", "name": "apply_patch", "input": whole}})
	input += frame(Object{"type": "response.completed", "response": Object{"status": "completed", "output": []any{}}})
	var args strings.Builder
	var call Part
	err := DecodeStream(Responses, strings.NewReader(input), func(e Event) error {
		if e.Kind == "delta" && e.Field == "arguments" {
			args.WriteString(e.Text)
		}
		if e.Kind == "part_done" {
			call = e.Part
		}
		return nil
	})
	var parsed Object
	if err != nil || decode([]byte(args.String()), &parsed) != nil || parsed["input"] != whole || customInput(call.Args) != whole {
		t.Fatal(args.String(), call, err)
	}
}

func TestReferenceEffortNormalizationAndFitting(t *testing.T) {
	if effort(" unrecognized ") != "" {
		t.Fatal("unknown effort must not become an upstream parameter")
	}
	model := Model{ID: "m", Metadata: &modelmeta.Resolved{Profile: Object{"reasoningEfforts": []string{"low", "high", "max", "ultra"}}}}
	for _, tt := range []struct{ asked, want string }{{"medium", "high"}, {"xhigh", "max"}, {"ultra", "max"}} {
		request := &Request{Source: Messages, Effort: tt.asked, Think: true, Turns: []Turn{{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}}}}
		out, err := Build(request, Responses, model, "")
		if err != nil || field(out, "reasoning", "effort") != tt.want {
			t.Fatal(tt, out, err)
		}
	}
	request := parseTest(t, Responses, Object{"model": "m", "input": []any{Object{"type": "configuration_update", "reasoning": Object{"effort": "high"}}, Object{"role": "user", "content": "hi"}}})
	if request.Effort != "" || request.Think {
		t.Fatal("a history setting must not turn on reasoning when the caller did not ask", request)
	}
}

func TestReferenceNativeResponsesInstructionRolesRemainInPlace(t *testing.T) {
	r := parseTest(t, Responses, Object{"model": "m", "input": []any{Object{"role": "developer", "content": "first"}, Object{"role": "user", "content": "hi"}, Object{"role": "assistant", "content": "hello"}, Object{"role": "developer", "content": "changed"}}})
	out := buildTest(t, r, Responses)
	roles := []string{}
	for _, item := range list(out["input"]) {
		roles = append(roles, str(field(item, "role")))
	}
	if !reflect.DeepEqual(roles, []string{"developer", "user", "assistant", "developer"}) || out["instructions"] != nil {
		t.Fatal(out)
	}
	translated := buildTest(t, r, Messages)
	if !strings.Contains(encode(translated), "system-reminder") || !strings.Contains(encode(translated["system"]), "first") {
		t.Fatal(translated)
	}
}

func TestReferenceCompactionDisablesAllToolsAndRejectsToolAnswers(t *testing.T) {
	r := &Request{Original: Object{"input": []any{Object{"type": "compaction_trigger"}}}, WebSearch: true, HostedTools: []any{Object{"googleSearch": Object{}}}, Tools: []Tool{{Name: "f"}}}
	if !prepareCompaction(r) || len(r.Tools) > 0 || r.WebSearch || len(r.HostedTools) > 0 {
		t.Fatal(r)
	}
	status := 0
	err := compactReply(Result{Stop: "tool", Parts: []Part{{Kind: "text", Text: "partial"}, {Kind: "call", Name: "f"}}}, Responses, "m", false, func(p Packet) error {
		if p.Type == "head" {
			status = p.Status
		}
		return nil
	})
	if err != nil || status != 502 {
		t.Fatal("tool answer must not become a completed checkpoint", status, err)
	}
}

func TestReferenceModelAliasesAreExactAndCapabilityIndependent(t *testing.T) {
	m := Model{ID: "anthropic/claude-opus-4.6-20260205", Enabled: true, Metadata: &modelmeta.Resolved{Profile: Object{"reasoningEfforts": []string{"low", "high"}}}}
	current := DesktopID(m)
	m.Metadata = &modelmeta.Resolved{Profile: Object{"reasoningEfforts": []any{}}}
	c := Call{DefaultModel: m.ID, Models: []Model{m}}
	if got, err := c.resolveModel(current + "[1m]"); err != nil || got.ID != m.ID {
		t.Fatal(got, err)
	}
	for _, fake := range []string{"uni-switch-0000000000.anthropic.claude-opus-4-6", "unrelated-claude-opus-4-6"} {
		if got, err := c.resolveModel(fake); err == nil {
			t.Fatal("unknown alias must not fall through to a tier", fake, got)
		}
	}
}

func TestReferenceNativeRelayRejectsAnUnfinishedStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frame(Object{"type": "response.output_text.delta", "output_index": 0, "delta": "partial"}))
	}))
	defer up.Close()
	c := callTest(t, up.URL)
	c.Upstream = Responses
	c.NativeRelay = true
	c.Body["stream"] = true
	completed := false
	err := NewService().Forward(context.Background(), c, func(p Packet) error { completed = completed || p.Type == "end"; return nil })
	if err == nil || completed {
		t.Fatal("EOF without a protocol terminal must not be successful", completed, err)
	}
}

func TestReferenceMessagesRepeatedIDsRoundTrip(t *testing.T) {
	body := Object{"messages": []any{Object{"role": "assistant", "content": []any{Object{"type": "tool_use", "id": "fc_0", "name": "f", "input": Object{}}}}}}
	ids := newMessageIDs(body)
	result := Result{Parts: []Part{{Kind: "call", ID: "fc_0", Name: "f", Args: Object{}}, {Kind: "call", ID: "fc_0", Name: "f", Args: Object{}}}}
	ids.result(&result)
	out := Render(result, Messages, "m", nil)
	first, second := str(field(list(out["content"])[0], "id")), str(field(list(out["content"])[1], "id"))
	if first == "fc_0" || second == first || rawID(first) != "fc_0" || rawID(second) != "fc_0" {
		t.Fatal(out)
	}
	request := parseTest(t, Messages, Object{"model": "m", "messages": []any{
		Object{"role": "assistant", "content": out["content"]},
		Object{"role": "user", "content": []any{
			Object{"type": "tool_result", "tool_use_id": first, "content": "ok"},
			Object{"type": "tool_result", "tool_use_id": second, "content": "ok"},
		}},
	}})
	if request.Turns[0].Parts[0].ID != "fc_0" || request.Turns[1].Parts[0].ID != "fc_0" {
		t.Fatal(request)
	}
	for _, id := range []string{repeatMark + "literal", "unsafe:中文", callMark + "literal"} {
		if rawID(safeID(id)) != id {
			t.Fatal("reserved vendor ID changed", id)
		}
	}
}

func TestReferenceNativeMessagesCollisionKeepsExtensionsAndRestoresResults(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		count := 0
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			var body Object
			if err := decodeReader(request.Body, &body); err != nil {
				t.Error(err)
				return
			}
			if count == 1 {
				messages := list(body["messages"])
				if field(list(field(messages[0], "content"))[0], "id") != "fc_0" || field(list(field(messages[1], "content"))[0], "tool_use_id") != "fc_0" {
					t.Error("salt went to vendor", body)
				}
			}
			count++
			block := Object{"type": "tool_use", "id": "fc_0", "name": "f", "input": Object{}, "vendor_extension": "retained"}
			if !streaming {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, encode(Object{"content": []any{block}, "stop_reason": "tool_use", "vendor_top": "retained"}))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ": keepalive\r\n\r\n"+frame(Object{"type": "content_block_start", "index": 0, "content_block": block})+frame(Object{"type": "content_block_stop", "index": 0})+frame(Object{"type": "message_delta", "delta": Object{"stop_reason": "tool_use"}})+frame(Object{"type": "message_stop"}))
		}))
		c := callTest(t, up.URL)
		c.Source, c.Upstream, c.NativeRelay = Messages, Messages, true
		c.Body = Object{"model": "synthetic-model", "stream": streaming, "messages": []any{Object{"role": "assistant", "content": []any{Object{"type": "tool_use", "id": "fc_0", "name": "f", "input": Object{}}}}, Object{"role": "user", "content": []any{Object{"type": "tool_result", "tool_use_id": "fc_0", "content": "prior"}}}}}
		status, data := collect(t, NewService(), c)
		if status != 200 || !strings.Contains(string(data), "vendor_extension") {
			t.Fatal(status, string(data))
		}
		var id string
		if streaming {
			if !strings.Contains(string(data), ": keepalive\r\n") {
				t.Fatal("comment lost")
			}
			err := ReadSSE(strings.NewReader(string(data)), func(f Frame) error {
				var v Object
				_ = decode([]byte(f.Data), &v)
				if field(v, "content_block", "type") == "tool_use" {
					id = str(field(v, "content_block", "id"))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			var v Object
			_ = decode(data, &v)
			id = str(field(list(v["content"])[0], "id"))
			if v["vendor_top"] != "retained" {
				t.Fatal(v)
			}
		}
		if id == "fc_0" || rawID(id) != "fc_0" {
			t.Fatal(id)
		}
		c.Body["messages"] = []any{Object{"role": "assistant", "content": []any{Object{"type": "tool_use", "id": id, "name": "f", "input": Object{}}}}, Object{"role": "user", "content": []any{Object{"type": "tool_result", "tool_use_id": id, "content": "next"}}}}
		collect(t, NewService(), c)
		up.Close()
	}
}

func TestReferenceNativeUnchangedSSEKeepsEveryByte(t *testing.T) {
	for _, nl := range []string{"\r", "\n", "\r\n"} {
		raw := "\xef\xbb\xbf: alive" + nl + nl + "id: 12" + nl + "event: response.completed" + nl + "data: {\"type\":\"response.completed\"," + nl + "data: \"response\":{\"status\":\"completed\",\"vendor\":true}}" + nl + nl
		response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(chopped{strings.NewReader(raw), 1})}
		var actual strings.Builder
		err := relayHTTP(context.Background(), response, func(p Packet) error {
			if p.Type == "data" {
				b, _ := base64.StdEncoding.DecodeString(p.Data)
				actual.Write(b)
			}
			return nil
		}, Responses, newMessageIDs(nil))
		if err != nil || actual.String() != raw {
			t.Fatalf("nl=%q: %q %v", nl, actual.String(), err)
		}
	}
}

func TestReferenceToolDeltaReachesClientBeforeUpstreamFinishes(t *testing.T) {
	for _, custom := range []bool{false, true} {
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			kind, deltaKind, initial := "function_call", "response.function_call_arguments.delta", `{"x":"中`
			if custom {
				kind, deltaKind, initial = "custom_tool_call", "response.custom_tool_call_input.delta", "*** Begin 中文"
			}
			item := Object{"type": kind, "call_id": "c", "name": "f"}
			_, _ = io.WriteString(w, frame(Object{"type": "response.output_item.added", "output_index": 0, "item": item})+frame(Object{"type": deltaKind, "output_index": 0, "delta": initial}))
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			if custom {
				item["input"] = initial
			} else {
				_, _ = io.WriteString(w, frame(Object{"type": deltaKind, "output_index": 0, "delta": `文"}`}))
				item["arguments"] = `{"x":"中文"}`
			}
			_, _ = io.WriteString(w, frame(Object{"type": "response.output_item.done", "output_index": 0, "item": item})+frame(Object{"type": "response.completed", "response": Object{"status": "completed", "output": []any{item}}}))
		}))
		c := callTest(t, up.URL)
		c.Source, c.Upstream = Messages, Responses
		c.Body = Object{"model": "synthetic-model", "stream": true, "messages": []any{Object{"role": "user", "content": "run"}}}
		done := make(chan error, 1)
		seen := make(chan struct{}, 1)
		var received strings.Builder
		go func() {
			done <- NewService().Forward(context.Background(), c, func(p Packet) error {
				if p.Type == "data" {
					b, _ := base64.StdEncoding.DecodeString(p.Data)
					received.Write(b)
					if strings.Contains(string(b), "input_json_delta") && strings.Contains(string(b), "中") {
						select {
						case seen <- struct{}{}:
						default:
						}
					}
				}
				return nil
			})
		}()
		select {
		case <-seen:
		case err := <-done:
			unblock()
			up.Close()
			t.Fatalf("stream stopped before tool delta: %v", err)
		case <-time.After(3 * time.Second):
			unblock()
			up.Close()
			t.Fatal("tool arguments buffered until upstream completion")
		}
		unblock()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
			if !strings.Contains(received.String(), "message_stop") || strings.Contains(received.String(), `"type":"error"`) {
				t.Error("tool stream failed to complete", received.String())
			}
		case <-time.After(3 * time.Second):
			t.Error("stream did not complete")
		}
		up.Close()
	}
}

func TestReferenceThinkingBudgetRespectsClientMaximum(t *testing.T) {
	for _, tt := range []struct {
		max           int
		effort        string
		budget, limit int
	}{{16384, "high", 15360, 16384}, {32000, "high", 24000, 32000}, {8192, "xhigh", 7168, 8192}, {0, "high", 24000, 28096}, {2048, "high", 0, 2048}} {
		r := &Request{Model: "m", Source: Responses, Max: tt.max, Effort: tt.effort, Think: true, Turns: []Turn{{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}}}}
		out := buildTest(t, r, Messages)
		if integer(field(out, "thinking", "budget_tokens")) != tt.budget || integer(out["max_tokens"]) != tt.limit {
			t.Fatal(tt, out)
		}
	}
}

func TestReferenceGeminiModelLastUsesSummaryAsk(t *testing.T) {
	for _, summary := range []bool{false, true} {
		r := &Request{Model: "m", Source: Responses, Turns: []Turn{{Role: "assistant", Parts: []Part{{Kind: "text", Text: "already said"}}}}}
		if summary {
			r.System = "You are a context summarization agent."
		}
		out := buildTest(t, r, Gemini)
		turns := list(out["contents"])
		text := str(field(list(field(turns[len(turns)-1], "parts"))[0], "text"))
		if field(turns[len(turns)-1], "role") != "user" || summary && !strings.Contains(text, "summary") || !summary && text != "Continue." {
			t.Fatal(out)
		}
	}
}

func TestReferenceNativeChatIDRepairsPreserveVendorData(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body Object
			_ = decodeReader(r.Body, &body)
			messages := list(body["messages"])
			call := obj(list(field(messages[0], "tool_calls"))[0])
			if str(call["id"]) == "" || field(messages[1], "tool_call_id") != call["id"] {
				t.Error("unpaired upstream history", body)
			}
			if !streaming {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, encode(Object{"vendor": "retained", "choices": []any{Object{"message": Object{"role": "assistant", "tool_calls": []any{Object{"function": Object{"name": "f", "arguments": "{}"}}, Object{"id": "nameless", "function": Object{"arguments": "{}"}}}}, "finish_reason": "tool_calls"}}}))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, fn := range []Object{{"name": "f", "arguments": "{"}, {"arguments": "}"}} {
				_, _ = io.WriteString(w, frame(Object{"vendor": "retained", "choices": []any{Object{"index": 0, "delta": Object{"tool_calls": []any{Object{"index": 0, "id": "", "function": fn}}}}}}))
			}
			_, _ = io.WriteString(w, frame(Object{"choices": []any{Object{"index": 0, "finish_reason": "tool_calls"}}})+"data: [DONE]\n\n")
		}))
		c := callTest(t, up.URL)
		c.Source, c.Upstream, c.NativeRelay = Chat, Chat, true
		c.Body = Object{"model": "synthetic-model", "stream": streaming, "messages": []any{Object{"role": "assistant", "tool_calls": []any{Object{"function": Object{"name": "f", "arguments": "{}"}}}}, Object{"role": "tool", "content": "done"}}}
		_, data := collect(t, NewService(), c)
		if !strings.Contains(string(data), "retained") {
			t.Fatal(string(data))
		}
		if streaming {
			fragments := 0
			err := ReadSSE(strings.NewReader(string(data)), func(f Frame) error {
				if f.Data == "[DONE]" {
					return nil
				}
				var v Object
				_ = decode([]byte(f.Data), &v)
				for _, ch := range list(v["choices"]) {
					for _, call := range list(field(ch, "delta", "tool_calls")) {
						if fragments == 0 && str(field(call, "id")) == "" {
							t.Error("first ID missing")
						}
						if fragments > 0 {
							if _, has := obj(call)["id"]; has {
								t.Error("later empty ID overwrites generated ID")
							}
						}
						fragments++
					}
				}
				return nil
			})
			if err != nil || fragments != 2 {
				t.Fatal(fragments, err)
			}
		} else {
			var v Object
			_ = decode(data, &v)
			calls := list(field(list(v["choices"])[0], "message", "tool_calls"))
			if len(calls) != 1 || str(field(calls[0], "id")) == "" {
				t.Fatal(v)
			}
		}
		up.Close()
	}
}

func TestReferenceGeminiNativeRetainsTrailingUsage(t *testing.T) {
	raw := frame(Object{"candidates": []any{Object{"content": Object{"role": "model", "parts": []any{Object{"text": "done"}}}, "finishReason": "STOP"}}}) + frame(Object{"usageMetadata": Object{"promptTokenCount": 10, "candidatesTokenCount": 2}, "vendor": "usage-tail"})
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(raw))}
	var output strings.Builder
	err := relayHTTP(context.Background(), response, func(p Packet) error {
		if p.Type == "data" {
			b, _ := base64.StdEncoding.DecodeString(p.Data)
			output.Write(b)
		}
		return nil
	}, Gemini, newMessageIDs(nil))
	if err != nil || output.String() != raw {
		t.Fatal(output.String(), err)
	}
}

type cutAfterData struct{ data string }

func (r *cutAfterData) Read(b []byte) (int, error) {
	if r.data == "" {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(b, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestReferenceValidLastFrameIsFlushedBeforeTransportError(t *testing.T) {
	text := ""
	err := DecodeStream(Responses, &cutAfterData{data: `data: {"type":"response.output_text.delta","output_index":0,"delta":"last 中文"}`}, func(e Event) error {
		if e.Kind == "delta" {
			text += e.Text
		}
		return nil
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) || text != "last 中文" {
		t.Fatal(text, err)
	}
}

func TestReferenceNativeStreamNeedsTerminalForEveryProtocol(t *testing.T) {
	for _, p := range []Protocol{Messages, Responses, Chat, Gemini} {
		var event Object
		switch p {
		case Responses:
			event = Object{"type": "response.output_text.delta", "delta": "partial"}
		case Messages:
			event = Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": "partial"}}
		case Chat:
			event = Object{"choices": []any{Object{"index": 0, "delta": Object{"content": "partial"}}}}
		case Gemini:
			event = Object{"candidates": []any{Object{"content": Object{"parts": []any{Object{"text": "partial"}}}}}}
		}
		response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(frame(event)))}
		end := false
		err := relayHTTP(context.Background(), response, func(packet Packet) error { end = end || packet.Type == "end"; return nil }, p, newMessageIDs(nil))
		if !errors.Is(err, errTruncated) || end {
			t.Fatal(p, err, end)
		}
	}
}

func TestNativeRelayIgnoresMalformedToolEntriesWithoutPanicking(t *testing.T) {
	tidy := nativeTidy{protocol: Chat, chatSeen: map[string]bool{}}
	input := Object{"choices": []any{Object{"index": 0, "delta": Object{"tool_calls": []any{nil, "bad", Object{"index": 0, "function": Object{"name": "f", "arguments": "{}"}}}}}}}
	var repaired Object
	if err := decode(tidy.event(Frame{Data: encode(input)}), &repaired); err != nil {
		t.Fatal(err)
	}
	calls := list(field(list(repaired["choices"])[0], "delta", "tool_calls"))
	if str(field(calls[2], "id")) == "" {
		t.Fatal(repaired)
	}
}
