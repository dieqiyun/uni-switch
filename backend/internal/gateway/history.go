package gateway

// Chat represents an assistant's text and calls in the same message. Responses
// represents them as separate items; coalesce only adjacent assistant messages
// at the Chat boundary, never reorder calls/results or system reminders.
func mergeChatAssistant(turns []any) []any {
	out := []any{}
	for _, v := range turns {
		m := obj(v)
		if len(out) > 0 && m["role"] == "assistant" && obj(out[len(out)-1])["role"] == "assistant" {
			last := obj(out[len(out)-1])
			if a := list(m["content"]); len(a) > 0 {
				last["content"] = append(list(last["content"]), a...)
			}
			if a := list(m["tool_calls"]); len(a) > 0 {
				last["tool_calls"] = append(list(last["tool_calls"]), a...)
			}
			if t := str(m["reasoning_content"]); t != "" {
				last["reasoning_content"] = str(last["reasoning_content"]) + t
			}
		} else {
			out = append(out, v)
		}
	}
	return out
}
