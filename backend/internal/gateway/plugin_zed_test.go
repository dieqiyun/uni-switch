package gateway

// zedParses is what Zed's cloud asks of an OpenAI Responses request
// (zed-industries/zed, crates/open_ai/src/responses.rs) before it goes on,
// "" when it would take it.
func zedParses(q map[string]any) string {
	items, _ := q["input"].([]any)
	for _, it := range items {
		im, _ := it.(map[string]any)
		switch ty, _ := im["type"].(string); ty {
		case "message":
			switch im["role"] {
			case "user", "assistant", "system", "tool":
			default:
				return "unknown variant `" + im["role"].(string) + "`, expected one of `user`, `assistant`, `system`, `tool`"
			}
			if _, ok := im["content"].([]any); !ok {
				return "invalid type: expected a sequence"
			}
		case "reasoning":
			if c, ok := im["content"]; ok && c == nil {
				return "invalid type: null, expected a sequence"
			}
		case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "compaction", "compaction_trigger":
		default:
			return "unknown variant `" + ty + "`"
		}
	}
	tools, _ := q["tools"].([]any)
	for _, t := range tools {
		if ty := t.(map[string]any)["type"]; ty != "function" && ty != "custom" {
			return "unknown variant `" + ty.(string) + "`, expected `function` or `custom`"
		}
	}
	inc, _ := q["include"].([]any)
	for _, v := range inc {
		if v != "reasoning.encrypted_content" {
			return "unknown variant `" + v.(string) + "`"
		}
	}
	return ""
}
