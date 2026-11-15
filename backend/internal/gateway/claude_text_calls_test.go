package gateway

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// The report's lines (Zhenzhen on Discord, Claude Desktop 3.1623.0 on
// "Fable 5.1 High", uni-switch 0.1.1327): the model wrote its calls in the
// form a conversation told as text gives past calls, ids made up.
const (
	reportedPlanCall = `[tool call mcp__uni-switch__mcp__ccd_turn__update_plan id=toolu_0Validate args={"advance":true}]`
	reportedBashCall = `[tool call mcp__uni-switch__Bash id=toolu_0Validate bash args={"command":"cd /Users/... && git diff --check ..."}]`
)

// textCallTurn is a streamed reply whose words are chunks, ended as Claude
// Code ends a turn: end_turn and its result.
func textCallTurn(id string, chunks ...string) []string {
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"` + id + `","model":"claude-fable-5-1","usage":{"input_tokens":3,"output_tokens":1}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
	}
	for _, c := range chunks {
		b, _ := json.Marshal(c)
		lines = append(lines, `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":`+string(b)+`}}}`)
	}
	return append(lines,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":40}}}`,
		`{"type":"stream_event","event":{"type":"message_stop"}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
	)
}

// realCallTurn is a streamed reply that calls the client's update_plan
// through the run's MCP helper, as Claude Code tells it.
var realCallTurn = []string{
	`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_real","model":"claude-fable-5-1","usage":{"input_tokens":3,"output_tokens":1}}}}`,
	`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01RealCall","name":"mcp__uni-switch__mcp__ccd_turn__update_plan","input":{}}}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"advance\":true}"}}}`,
	`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
	`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}}`,
	`{"type":"stream_event","event":{"type":"message_stop"}}`,
}

// stdinOf gives run an input that records each line it is told.
func stdinOf(t *testing.T, run *subscriptionRun) func() []string {
	t.Helper()
	w := &linesTold{}
	run.stdin = w
	return w.lines
}

// linesTold keeps what a run is told as each write is made, so a test reads
// it the moment the reply is in. A pipe's write returned before its reader
// had kept the line, and the check could come first (CI, 843417957).
type linesTold struct {
	mu   sync.Mutex
	part string
	told []string
}

func (w *linesTold) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part += string(b)
	for {
		i := strings.IndexByte(w.part, '\n')
		if i < 0 {
			return len(b), nil
		}
		w.told = append(w.told, w.part[:i])
		w.part = w.part[i+1:]
	}
}

func (w *linesTold) Close() error { return nil }

func (w *linesTold) lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.told...)
}

// A reply that writes its tool calls as "[tool call …]" text shows the
// client none of it: the run is told to make the calls, and the client gets
// the real call, the words before the text calls kept. Before, the client
// showed the lines as the reply's words and nothing ran.
func TestClaudeTextToolCallsAreMadeForReal(t *testing.T) {
	run := &subscriptionRun{}
	told := stdinOf(t, run)
	lines := append(textCallTurn("msg_text",
		"Plan advanced; now validating.\n",
		"[tool", " call mcp__uni-switch__mcp__ccd_turn__update_plan id=toolu_0Valid",
		strings.TrimPrefix(reportedPlanCall, "[tool call mcp__uni-switch__mcp__ccd_turn__update_plan id=toolu_0Valid")+"\n",
		reportedBashCall+"\n[tool result id=toolu_0Validate]\nok"),
		realCallTurn...)
	text, stop, _, _, raw, ended, ok := claudeReply(t, run, lines, true)
	if strings.Contains(raw, "[tool call") || strings.Contains(raw, "tool result id=") {
		t.Fatalf("the client was shown the calls written as text:\n%s", raw)
	}
	if text != "Plan advanced; now validating.\n[mcp__ccd_turn__update_plan]" || stop != "tool_use" || ended != "tool" || !ok {
		t.Fatalf("reply %q stop %q, run ended %q %v:\n%s", text, stop, ended, ok, raw)
	}
	if strings.Count(raw, `"type":"message_start"`) != 1 {
		t.Fatalf("the reply didn't go on as one message:\n%s", raw)
	}
	asks := told()
	if len(asks) != 1 {
		t.Fatalf("the run was told %d lines, want the one ask: %q", len(asks), asks)
	}
	var ask struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(asks[0]), &ask) != nil || ask.Type != "user" || ask.Message.Role != "user" ||
		len(ask.Message.Content) != 1 || ask.Message.Content[0].Text != textCallsAsk {
		t.Fatalf("the run was told %s", asks[0])
	}
	// the ask names neither uni-switch nor the vendor
	for _, w := range []string{"uni-switch", "Claude", "Anthropic"} {
		if strings.Contains(textCallsAsk, w) || strings.Contains(pastCallsNote, w) {
			t.Fatalf("the text uni-switch adds names %s", w)
		}
	}
}

// Told once, a reply that writes its calls as text again reaches the
// client as written: the run isn't asked over and over.
func TestClaudeTextToolCallsAskedOnce(t *testing.T) {
	run := &subscriptionRun{}
	told := stdinOf(t, run)
	lines := append(textCallTurn("msg_text", reportedPlanCall), textCallTurn("msg_again", "Calling it: \n"+reportedBashCall)...)
	text, stop, _, _, raw, ended, ok := claudeReply(t, run, lines, true)
	if text != "Calling it: \n"+reportedBashCall || stop != "end_turn" || ended != "stop" || !ok {
		t.Fatalf("reply %q stop %q, run ended %q %v:\n%s", text, stop, ended, ok, raw)
	}
	if n := len(told()); n != 1 {
		t.Fatalf("the run was asked %d times", n)
	}
}

// A run with no input to tell (one that ends by itself) gives the client the
// words as written, as before.
func TestClaudeTextToolCallsWithoutInput(t *testing.T) {
	text, stop, _, _, raw, _, _ := claudeReply(t, &subscriptionRun{}, textCallTurn("msg_text", "a\n"+reportedPlanCall), true)
	if text != "a\n"+reportedPlanCall || stop != "end_turn" {
		t.Fatalf("reply %q stop %q:\n%s", text, stop, raw)
	}
}

// Words that only look like the start of such a line, or name it mid-line,
// go to the client whole, however they are cut into chunks.
func TestClaudeTextCallsHoldsOnlyTheLine(t *testing.T) {
	for _, words := range []string{
		"[tooling] fixed\n[tool]\n  [tool calls] are made with tools",
		"see [tool call x id=1 args={}] in the log\nok",
		"",
		"\n\n[tool ca",
		"一只猫\n  [工具] 调用 …\n[tool callé]",
	} {
		for size := 1; size <= len(words)+1; size++ {
			var c textCalls
			var out strings.Builder
			for i := 0; i < len(words); i += size {
				out.WriteString(c.push(words[i:min(i+size, len(words))]))
			}
			held, seen := c.end()
			if seen || out.String()+held != words {
				t.Fatalf("%q in chunks of %d: shown %q, held %q (%v)", words, size, out.String(), held, seen)
			}
		}
	}
	var c textCalls
	shown := c.push("ok\n  ") + c.push(reportedPlanCall[:3]) + c.push(reportedPlanCall[3:]+"\nmore")
	if held, seen := c.end(); shown != "ok\n" || !seen || held != "  "+reportedPlanCall+"\nmore" {
		t.Fatalf("shown %q, held %q (%v)", shown, held, seen)
	}
}

// A conversation told as text says its "[tool call …]" lines are a
// record, and that writing one calls nothing: for a run started anew and
// for a turn told with the client's own calls. One with no calls is told
// as before.
func TestClaudePromptSaysPastCallsAreARecord(t *testing.T) {
	msgs := []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "check the diff"}}},
		{Role: "assistant", Parts: []Part{{Kind: ToolCall, ID: "toolu_01", Name: "Bash", Args: json.RawMessage(`{"command":"git diff --check"}`)}}},
		{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: "toolu_01", Text: "clean"}}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "clean"}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "go on"}}},
	}
	tools := []Tool{{Name: "Bash"}}
	all := func(blocks []map[string]any) string {
		var s strings.Builder
		for _, b := range blocks {
			t, _ := b["text"].(string)
			s.WriteString(t)
		}
		return s.String()
	}
	blocks, err := renderClaudePrompt(&Request{Messages: msgs, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if got := all(blocks); !strings.HasSuffix(got, pastCallsNote+"\n\n</current_turn>") || !strings.Contains(got, "[tool call mcp__uni-switch__Bash id=toolu_01") {
		t.Fatalf("started anew:\n%s", got)
	}
	if got := all(renderClaudeTurn(msgs[1:3], tools)); !strings.HasSuffix(got, "\n\n"+pastCallsNote) {
		t.Fatalf("the turn since:\n%s", got)
	}
	for _, m := range [][]Message{{msgs[0]}, {msgs[0], msgs[3], msgs[4]}} {
		blocks, _ := renderClaudePrompt(&Request{Messages: m, Tools: tools})
		if got := all(blocks) + all(renderClaudeTurn(m, tools)); strings.Contains(got, pastCallsNote) {
			t.Fatalf("no calls, yet told:\n%s", got)
		}
	}
}

// The same told in whole messages, as Claude Code tells an answer it asked
// for again without a stream.
func TestClaudeTextToolCallsUnstreamed(t *testing.T) {
	run := &subscriptionRun{}
	told := stdinOf(t, run)
	words, _ := json.Marshal("Validating.\n" + reportedBashCall)
	lines := append([]string{
		`{"type":"assistant","message":{"id":"msg_whole","model":"claude-fable-5-1","role":"assistant","content":[{"type":"text","text":` + string(words) + `}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":40}}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
	}, realCallTurn...)
	text, stop, _, _, raw, ended, ok := claudeReply(t, run, lines, true)
	if text != "Validating.\n[mcp__ccd_turn__update_plan]" || stop != "tool_use" || ended != "tool" || !ok || len(told()) != 1 {
		t.Fatalf("reply %q stop %q, run ended %q %v, told %q:\n%s", text, stop, ended, ok, told(), raw)
	}
}

// Claude's own call written out as text (packinggoodsllc_07668: Claude Code
// through a group of Claude models) is held back and asked for, as a
// "[tool call …]" line is: bare or in its <function_calls>.
func TestClaudeTextInvokeCallsAreMadeForReal(t *testing.T) {
	for _, written := range []string{
		invokeReported,
		"<function_calls>\n" + `<invoke name="ToolSearch"><parameter name="query">select:mcp__uni-switch__uni-switch-browser__browser_navigate</parameter></invoke>` + "\n</function_calls>",
	} {
		run := &subscriptionRun{}
		told := stdinOf(t, run)
		lines := append(textCallTurn("msg_text", "好的。\n", written[:5], written[5:]), realCallTurn...)
		text, stop, _, _, raw, ended, ok := claudeReply(t, run, lines, true)
		if strings.Contains(raw, "invoke") || strings.Contains(raw, "Result:") {
			t.Fatalf("the client was shown the calls written as text:\n%s", raw)
		}
		if text != "好的。\n[mcp__ccd_turn__update_plan]" || stop != "tool_use" || ended != "tool" || !ok || len(told()) != 1 {
			t.Fatalf("reply %q stop %q, run ended %q %v, told %q:\n%s", text, stop, ended, ok, told(), raw)
		}
	}
	// a line that only names the form mid-line, or starts like it, goes on
	var c textCalls
	words := "use <invoke name=\"x\"> to call\n<invoked> later\n<function_call"
	shown := c.push(words)
	if held, seen := c.end(); seen || shown+held != words {
		t.Fatalf("shown %q, held %q (%v)", shown, held, seen)
	}
}
