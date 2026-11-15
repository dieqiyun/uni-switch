package gateway

import (
	"encoding/json"
	"fmt"

	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// largeConversation is a long agent conversation as its client sends it:
// a system prompt, tools with schemas, and turns of text, tool calls and
// tool results, about words of text in all (~4 bytes a token).
func largeConversation(proto provider.Protocol, model string, words int) []byte {
	para := strings.Repeat("The function reads the file, splits it on newlines and returns the lines that match. ", 12)
	type schema = map[string]any
	var tools []schema
	for i := range 30 {
		props := schema{"path": schema{"type": "string", "description": "Absolute path to the file " + fmt.Sprint(i)}, "limit": schema{"type": "integer"}}
		switch proto {
		case provider.Anthropic:
			tools = append(tools, schema{"name": fmt.Sprintf("Tool%d", i), "description": para[:400], "input_schema": schema{"type": "object", "properties": props, "required": []string{"path"}}})
		case provider.Chat:
			tools = append(tools, schema{"type": "function", "function": schema{"name": fmt.Sprintf("Tool%d", i), "description": para[:400], "parameters": schema{"type": "object", "properties": props, "required": []string{"path"}}}})
		default:
			tools = append(tools, schema{"type": "function", "name": fmt.Sprintf("Tool%d", i), "description": para[:400], "parameters": schema{"type": "object", "properties": props, "required": []string{"path"}}})
		}
	}
	var msgs []any
	n := 0
	for turn := 0; n < words*6; turn++ {
		id := fmt.Sprintf("call_%04d", turn)
		args := fmt.Sprintf(`{"path":"/src/pkg/file%d.go","limit":200}`, turn)
		result := para + strings.Repeat("line of code with \"quotes\" and unicode é 漢字\n", 20)
		n += len(para)*2 + len(result)
		switch proto {
		case provider.Anthropic:
			msgs = append(msgs,
				schema{"role": "user", "content": []any{schema{"type": "text", "text": para}}},
				schema{"role": "assistant", "content": []any{schema{"type": "text", "text": para}, schema{"type": "tool_use", "id": id, "name": "Tool1", "input": json.RawMessage(args)}}},
				schema{"role": "user", "content": []any{schema{"type": "tool_result", "tool_use_id": id, "content": result}}})
		case provider.Chat:
			msgs = append(msgs,
				schema{"role": "user", "content": para},
				schema{"role": "assistant", "content": para, "tool_calls": []any{schema{"id": id, "type": "function", "function": schema{"name": "Tool1", "arguments": args}}}},
				schema{"role": "tool", "tool_call_id": id, "content": result})
		default:
			msgs = append(msgs,
				schema{"type": "message", "role": "user", "content": []any{schema{"type": "input_text", "text": para}}},
				schema{"type": "message", "role": "assistant", "content": []any{schema{"type": "output_text", "text": para}}},
				schema{"type": "function_call", "call_id": id, "name": "Tool1", "arguments": args},
				schema{"type": "function_call_output", "call_id": id, "output": result})
		}
	}
	body := schema{"model": model, "max_tokens": 32000, "stream": false, "tools": tools}
	switch proto {
	case provider.Anthropic:
		msgs[len(msgs)-1] = schema{"role": "user", "content": []any{schema{"type": "text", "text": "go on"}}}
		body["system"] = []any{schema{"type": "text", "text": para + para, "cache_control": schema{"type": "ephemeral"}}}
		body["messages"] = msgs
	case provider.Chat:
		// the user's message follows the last call's answer: a call left
		// unanswered is mended before it is relayed (toolPairsOut)
		msgs = append(msgs, schema{"role": "user", "content": "go on"})
		body["messages"] = append([]any{schema{"role": "system", "content": para + para}}, msgs...)
	default:
		delete(body, "max_tokens")
		body["instructions"] = para + para
		body["input"] = msgs
	}
	b, _ := json.Marshal(body)
	return b
}
