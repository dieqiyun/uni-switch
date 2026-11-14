package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// #1945 (XcodeFish, GLM under Codex): what GLM leaves between a call's
// JSON and its </tool_call> went out as text beside the call. The tails are
// the ones the issue lists, as Codex showed them.
func TestTextWrittenCallTailDropped(t *testing.T) {
	for _, tail := range []string{"]", "}", `]}]}}"}}}}`, "}}", `">`, "\n}}"} {
		text, calls, finish, raw := relayedChat(t, `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}],`+textCallTools+`}`,
			chunkOf("Listing it.\n<tool_call>"),
			chunkOf(`{"name":"bash","arguments":{"command":"ls"}}`+tail),
			chunkOf("</tool_call>"),
			stopChunk)
		if text != "Listing it.\n" || calls["bash"] != `{"command":"ls"}` || finish != "tool_calls" {
			t.Errorf("%q: text %q calls %v finish %q\n%s", tail, text, calls, finish, raw)
		}
	}
	// and a tail after a call's own close
	text, calls, _, raw := relayedChat(t, `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}],`+textCallTools+`}`,
		chunkOf(`<tool_call>{"name":"bash","arguments":{"command":"ls"}}</tool_call>`+"\n"+`}}</tool_call>`),
		stopChunk)
	if text != "" || calls["bash"] != `{"command":"ls"}` {
		t.Errorf("after the close: text %q calls %v\n%s", text, calls, raw)
	}
}

// A heredoc whose quotes the model left unescaped: the JSON object closed
// early, at a quote inside the command, and the command went to the agent
// cut short (#1945). Such a block is no call: the text goes on as it was,
// for the agent to see and the model to write again.
func TestTextWrittenCallCutEarlyNotCalled(t *testing.T) {
	block := `<tool_call>{"name":"bash","arguments":{"command":"python3 - <<'PY'\nimport json\nprint(json.dumps({\"名\": 'v', \"n\": {\"x\": \"y"}}))\nPY\n | tail -8","timeout":180000}}</tool_call>`
	text, calls, finish, raw := relayedChat(t, `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}],`+textCallTools+`}`,
		chunkOf(block), stopChunk)
	if len(calls) != 0 || finish != "stop" || text != block {
		var a map[string]any
		json.Unmarshal([]byte(calls["bash"]), &a)
		t.Fatalf("text %q calls %v (%v) finish %q\n%s", text, calls, a, finish, raw)
	}
	// prose after a call with no close of its own: the call still reads
	// as before; the prose, said before anything ran, goes
	// (packinggoodsllc_07668, parseTextCalls)
	text, calls, _, _ = relayedChat(t, `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}],`+textCallTools+`}`,
		chunkOf(`<tool_call>{"name":"bash","arguments":{"command":"ls"}}`+"\nDone."), stopChunk)
	if calls["bash"] != `{"command":"ls"}` || strings.Contains(text, "Done.") {
		t.Fatalf("no close: text %q calls %v", text, calls)
	}
}
