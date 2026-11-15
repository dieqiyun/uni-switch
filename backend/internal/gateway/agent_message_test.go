package gateway

import (
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// MultiAgentV2 reads a namespaced call's message as sealed unless the call
// says none of its arguments are; nothing uni-switch serves seals them.
func TestNamespacedCallArgsArePlain(t *testing.T) {
	res := Result{Parts: []Part{
		{Kind: ToolCall, ID: "c1", Name: "collaboration__spawn_agent", Args: json.RawMessage(`{"message":"hi"}`)},
		{Kind: ToolCall, ID: "c2", Name: "exec_command", Args: json.RawMessage(`{}`)},
	}}
	var out struct {
		Output []map[string]any `json:"output"`
	}
	if err := json.Unmarshal(render(provider.Responses, res, namespacedReq()), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Output) != 2 {
		t.Fatalf("output = %v", out.Output)
	}
	if enc, ok := out.Output[0]["encrypted_function_args"].([]any); !ok || len(enc) != 0 {
		t.Fatalf("namespaced call = %v", out.Output[0])
	}
	if _, has := out.Output[1]["encrypted_function_args"]; has {
		t.Fatalf("top-level call = %v", out.Output[1])
	}
}

// A subagent's task comes as an agent_message item; it is the agent's user turn.
func TestAgentMessageIsUserTurn(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m","input":[
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"be brief"}]},
		{"type":"agent_message","author":"/root","recipient":"/root/w","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\nreply PONG"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Messages) != 1 || r.Messages[0].Role != "user" || text(r.Messages[0].Parts) != "Message Type: NEW_TASK\nPayload:\nreply PONG" {
		t.Fatalf("messages = %+v", r.Messages)
	}
}
