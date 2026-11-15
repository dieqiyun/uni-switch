package gateway

import (
	"encoding/json"

	"testing"
)

// agentMessageTurn is a lead's turn with its subagent's plain reply in it,
// between the call that sent the task and its output, as Codex's
// MultiAgentV2 hands it back, with no tools.
const agentMessageTurn = `"input":[
	{"type":"message","role":"user","content":"go"},
	{"type":"function_call","call_id":"c1","name":"send_message","arguments":"{}"},
	{"type":"agent_message","author":"/root/research","recipient":"/root","content":[{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\nfound the cause"}]},
	{"type":"function_call_output","call_id":"c1","output":"sent"}]`

// checkAgentMessageSent fails unless Grok was given the subagent's reply
// as the user's message, after the run it broke, its sender and recipient
// first. The reply sat between the call and its output on the way in;
// joinToolRuns puts it after the output, so the exchange Grok reads is
// whole. Its text, its role, and its conversion are as they were.
func checkAgentMessageSent(t *testing.T, q map[string]any) {
	t.Helper()
	in, _ := q["input"].([]any)
	if len(in) != 4 {
		t.Fatalf("Grok was given %v", q["input"])
	}
	if o, _ := in[2].(map[string]any); o["type"] != "function_call_output" || o["call_id"] != "c1" {
		t.Fatalf("its call's output went as %v", in[2])
	}
	m, _ := in[3].(map[string]any)
	got, _ := json.Marshal(m)
	if want := `{"content":[{"text":"From /root/research to /root\n\n","type":"input_text"},{"text":"Message Type: FINAL_ANSWER\nPayload:\nfound the cause","type":"input_text"}],"role":"user","type":"message"}`; string(got) != want {
		t.Fatalf("the agent_message went as\n%s\nwant\n%s", got, want)
	}
}
