package gateway

import (
	"strings"
	"testing"
)

func TestGeminiToolsImagesSchemaAndSameNameResults(t *testing.T) {
	r := parseTest(t, Gemini, Object{"model": "m", "systemInstruction": Object{"parts": []any{Object{"text": "system"}}}, "contents": []any{
		Object{"parts": []any{Object{"text": "ask"}, Object{"inlineData": Object{"mimeType": "image/png", "data": "synthetic"}}}},
		Object{"role": "model", "parts": []any{Object{"functionCall": Object{"id": "first", "name": "shell", "args": Object{"n": 1}}}, Object{"functionCall": Object{"name": "shell", "args": Object{"n": 2}}}}},
		Object{"role": "user", "parts": []any{Object{"functionResponse": Object{"id": "first", "name": "shell", "response": Object{"text": "one"}}}, Object{"functionResponse": Object{"name": "shell", "response": Object{"text": "two"}}}}},
	}, "tools": []any{Object{"functionDeclarations": []any{Object{"name": "shell", "parameters": Object{"type": "OBJECT", "properties": Object{"n": Object{"type": "INTEGER"}}}}}}}, "generationConfig": Object{"maxOutputTokens": 100, "responseMimeType": "application/json", "responseSchema": Object{"type": "OBJECT"}}, "toolConfig": Object{"functionCallingConfig": Object{"mode": "ANY", "allowedFunctionNames": []any{"shell"}}}})
	if r.Turns[1].Parts[0].ID != r.Turns[2].Parts[0].ID || r.Turns[1].Parts[1].ID != r.Turns[2].Parts[1].ID || r.Turns[2].Parts[0].ID == r.Turns[2].Parts[1].ID {
		t.Fatal("tool responses were not paired")
	}
	for _, to := range []Protocol{Messages, Responses, Chat} {
		v := buildTest(t, r, to)
		s := encode(v)
		if !strings.Contains(s, "one") || !strings.Contains(s, "two") || !strings.Contains(s, "image") || !strings.Contains(s, "system") || strings.Contains(s, "INTEGER") {
			t.Fatal(s)
		}
	}
	if field(r.Format, "schema", "type") != "object" || r.Choice != "name:shell" {
		t.Fatal(r.Format, r.Choice)
	}
}

func TestGeminiCarriesSignedAndOpaqueReasoningThroughToolTurns(t *testing.T) {
	for _, part := range []Part{
		{Kind: "thinking", Text: "think", Signature: "synthetic-signature", SealedBy: "messages"},
		{Kind: "thinking", SealedBy: "responses", Raw: Object{"type": "reasoning", "id": "rs", "encrypted_content": "synthetic-opaque", "summary": []any{}}},
	} {
		got := Render(Result{ID: "reply", Stop: "tool", Parts: []Part{part, {Kind: "call", ID: "call", Name: "shell", Args: Object{"x": 1}}}}, Gemini, "m", nil)
		content := obj(field(list(got["candidates"])[0], "content"))
		user := Object{"role": "user", "parts": []any{Object{"text": "ask"}}}
		result := Object{"functionResponse": Object{"id": "call", "name": "shell", "response": Object{"ok": true}}}
		reply := Object{"role": "user", "parts": []any{result}}
		r := parseTest(t, Gemini, Object{"model": "m", "contents": []any{user, content, reply}})
		to, needle := Messages, "synthetic-signature"
		if part.SealedBy == "responses" {
			to, needle = Responses, "synthetic-opaque"
		}
		v := buildTest(t, r, to)
		if !strings.Contains(encode(v), needle) {
			t.Fatal(encode(v))
		}
	}
}

func TestGeminiStreamInitialTextUsageAndTruncation(t *testing.T) {
	in := frame(Object{"type": "message_start", "message": Object{"usage": Object{"input_tokens": 3}}}) + frame(Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": "initial"}}) + frame(Object{"type": "content_block_delta", "index": 0, "delta": Object{"type": "text_delta", "text": "中文"}}) + frame(Object{"type": "content_block_stop", "index": 0}) + frame(Object{"type": "message_delta", "delta": Object{"stop_reason": "end_turn"}, "usage": Object{"output_tokens": 2}}) + frame(Object{"type": "message_stop"})
	for n := 1; n < 8; n++ {
		var frames []Frame
		enc := NewEncoder(Gemini, "m", nil, func(f Frame) error { frames = append(frames, f); return nil })
		if e := enc.Start(); e != nil {
			t.Fatal(e)
		}
		if e := DecodeStream(Messages, chopped{strings.NewReader(in), n}, enc.Accept); e != nil {
			t.Fatal(e)
		}
		text := ""
		var last Object
		for _, f := range frames {
			if e := decode([]byte(f.Data), &last); e != nil {
				t.Fatal(e)
			}
			for _, p := range list(field(list(last["candidates"])[0], "content", "parts")) {
				text += str(obj(p)["text"])
			}
		}
		if text != "initial中文" || integer(field(last, "usageMetadata", "promptTokenCount")) != 3 || field(list(last["candidates"])[0], "finishReason") != "STOP" {
			t.Fatal(text, last)
		}
	}
	if DecodeStream(Messages, strings.NewReader(in[:len(in)/2]), func(Event) error { return nil }) == nil {
		t.Fatal("truncated response succeeded")
	}
}

func TestGeminiRejectsUnsupportedPartsAndMalformedCarry(t *testing.T) {
	for _, part := range []Object{{"unsupportedPart": Object{"data": "synthetic"}}, {"functionCall": Object{"name": "shell"}, "thoughtSignature": geminiSeal + "broken"}} {
		if _, e := Parse(Gemini, Object{"model": "m", "contents": []any{Object{"role": "user", "parts": []any{part}}}}); e == nil {
			t.Fatal("silently dropped unsupported input")
		}
	}
	for _, status := range []int{400, 401, 403, 404, 429, 500, 502, 503, 504} {
		if str(field(failure(Gemini, status, "synthetic"), "error", "status")) == "" {
			t.Fatal(status)
		}
	}
}
