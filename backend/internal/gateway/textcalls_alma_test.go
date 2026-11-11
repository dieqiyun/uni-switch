package gateway

import (
	"encoding/json"
)

// dsmlResponsesSSE is a Responses stream as DeepSeek's reads: a reasoning
// item, then a message item whose text is text, one character to a delta.
func dsmlResponsesSSE(text string) string {
	ev := func(typ string, v map[string]any) string {
		v["type"] = typ
		b, _ := json.Marshal(v)
		return "event: " + typ + "\ndata: " + string(b)
	}
	seq := 0
	n := func(v map[string]any) map[string]any { v["sequence_number"] = seq; seq++; return v }
	reasoning := map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{map[string]any{"type": "summary_text", "text": "need a search"}}}
	part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
	msg := map[string]any{"type": "message", "role": "assistant", "id": "msg_1", "status": "completed", "content": []any{part}}
	lines := []string{
		ev("response.created", n(map[string]any{"response": map[string]any{"id": "r1", "model": "deepseek-flash", "status": "in_progress", "output": []any{}}})),
		ev("response.output_item.added", n(map[string]any{"output_index": 0, "item": map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{}}})),
		ev("response.reasoning_summary_text.delta", n(map[string]any{"item_id": "rs_1", "output_index": 0, "summary_index": 0, "delta": "need a search"})),
		ev("response.output_item.done", n(map[string]any{"output_index": 0, "item": reasoning})),
		ev("response.output_item.added", n(map[string]any{"output_index": 1,
			"item": map[string]any{"type": "message", "role": "assistant", "id": "msg_1", "status": "in_progress", "content": []any{}}})),
		ev("response.content_part.added", n(map[string]any{"item_id": "msg_1", "output_index": 1, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})),
	}
	for _, r := range text {
		lines = append(lines, ev("response.output_text.delta", n(map[string]any{"item_id": "msg_1", "output_index": 1, "content_index": 0, "delta": string(r)})))
	}
	lines = append(lines,
		ev("response.output_text.done", n(map[string]any{"item_id": "msg_1", "output_index": 1, "content_index": 0, "text": text})),
		ev("response.content_part.done", n(map[string]any{"item_id": "msg_1", "output_index": 1, "content_index": 0, "part": part})),
		ev("response.output_item.done", n(map[string]any{"output_index": 1, "item": msg})),
		ev("response.completed", n(map[string]any{"response": map[string]any{"id": "r1", "model": "deepseek-flash", "status": "completed",
			"output": []any{reasoning, msg}, "usage": map[string]any{"input_tokens": 900, "output_tokens": 120, "total_tokens": 1020}}})))
	return sse(lines...)
}
