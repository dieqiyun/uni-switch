package gateway

import (
	"strings"
	"testing"
)

func TestChatRepeatedIDsInterleavedCallsAndFirstChoice(t *testing.T) {
	input := frame(Object{"choices": []any{Object{"index": "1", "delta": Object{"reasoning_content": "think", "reasoning": "think"}}}})
	chunk := func(index any, id, name, args string) string {
		return frame(Object{"choices": []any{Object{"index": 1, "delta": Object{"tool_calls": []any{Object{"index": index, "id": id, "function": Object{"name": name, "arguments": args}}}}}, Object{"index": 2, "delta": Object{"content": "ignore"}}}})
	}
	input += chunk(0, "c0", "first", `{"x":`) + chunk(1, "c1", "second", `{"y":`) + chunk(0, "c0", "first", `1}`) + chunk(1, "c1", "second", `2}`)
	input += frame(Object{"choices": []any{Object{"index": 1, "delta": Object{}, "finish_reason": "tool_calls"}}, "usage": Object{"prompt_tokens": 30, "completion_tokens": 8}}) + "data: [DONE]\n\n"
	var frames []Frame
	enc := NewEncoder(Messages, "m", nil, func(f Frame) error { frames = append(frames, f); return nil })
	_ = enc.Start()
	if e := DecodeStream(Chat, strings.NewReader(input), enc.Accept); e != nil {
		t.Fatal(e)
	}
	active := false
	calls := []string{}
	thinking := ""
	for _, f := range frames {
		var v Object
		_ = decode([]byte(f.Data), &v)
		switch v["type"] {
		case "content_block_start":
			if active {
				t.Fatal("interleaved Messages block", frames)
			}
			active = true
			if field(v, "content_block", "type") == "tool_use" {
				calls = append(calls, str(field(v, "content_block", "id")))
			}
		case "content_block_delta":
			thinking += str(field(v, "delta", "thinking"))
		case "content_block_stop":
			if !active {
				t.Fatal("unmatched stop")
			}
			active = false
		}
	}
	if len(calls) != 2 || calls[0] != "c0" || calls[1] != "c1" || thinking != "think" {
		t.Fatal(calls, thinking)
	}
}
func TestMessagesInitialUsageContainsInputAndCache(t *testing.T) {
	var frames []Frame
	e := NewEncoder(Messages, "m", nil, func(f Frame) error { frames = append(frames, f); return nil })
	_ = e.Start()
	if err := DecodeStream(Messages, strings.NewReader(messageStream("ok")), e.Accept); err != nil {
		t.Fatal(err)
	}
	var start Object
	_ = decode([]byte(frames[0].Data), &start)
	if integer(field(start, "message", "usage", "input_tokens")) != 12 || integer(field(start, "message", "usage", "cache_read_input_tokens")) != 3 {
		t.Fatal(start)
	}
}
func TestDelayedResponsesCallIdentityAndOpaque(t *testing.T) {
	input := frame(Object{"type": "response.output_item.added", "output_index": 0, "item": Object{"type": "reasoning", "id": "rs"}}) + frame(Object{"type": "response.reasoning_summary_text.delta", "output_index": 0, "delta": "first"}) + frame(Object{"type": "response.reasoning_summary_text.delta", "output_index": 0, "delta": " second"}) + frame(Object{"type": "response.output_item.done", "output_index": 0, "item": Object{"type": "reasoning", "id": "rs", "summary": []any{Object{"type": "summary_text", "text": "first second"}}, "encrypted_content": "sealed"}}) + frame(Object{"type": "response.output_item.added", "output_index": 1, "item": Object{"type": "function_call", "id": "item"}}) + frame(Object{"type": "response.function_call_arguments.delta", "output_index": 1, "delta": `{"p":" a  b "}`}) + frame(Object{"type": "response.output_item.done", "output_index": 1, "item": Object{"type": "function_call", "id": "item", "call_id": "call", "name": "tool", "arguments": `{"p":" a  b "}`}}) + frame(Object{"type": "response.completed", "response": Object{"status": "completed", "usage": Object{}}})
	var got []Part
	events := 0
	e := DecodeStream(Responses, strings.NewReader(input), func(e Event) error {
		if e.Kind == "part_done" {
			got = append(got, e.Part)
		}
		if e.Kind == "part" && e.Part.Kind == "call" {
			events++
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 2 || got[0].Raw["encrypted_content"] != "sealed" || got[0].Text != "first second" || got[1].ID != "call" || field(got[1].Args, "p") != " a  b " || events != 1 {
		t.Fatal(got, events)
	}
}
