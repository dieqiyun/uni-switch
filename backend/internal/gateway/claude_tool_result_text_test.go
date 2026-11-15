package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The message Claude Code's AgentSummary sends, as #1878 gives it: the
// result of the sub-agent's last call, and the label it asks for as a text
// block of its own after it.
const (
	summaryAsk     = "Describe your most recent action in 3-5 words. Do not use tools."
	summaryRequest = `{"model":"claude-sonnet-5","max_tokens":64,"messages":[` +
		`{"role":"user","content":"start the task"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"Bash","input":{"command":"tail -1 src"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"SOURCE_LAST_LINE"},{"type":"text","text":"` + summaryAsk + `"}]}]}`
	// a tool whose output says the same words: they are what it printed
	toolSaysRequest = `{"model":"claude-sonnet-5","max_tokens":64,"messages":[` +
		`{"role":"user","content":"start the task"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"Bash","input":{"command":"cat notes"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"SOURCE_LAST_LINE\n` + summaryAsk + `"}]}]}`
)

func parsed(t *testing.T, body string) *Request {
	t.Helper()
	req, err := parse(provider.Anthropic, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func promptText(t *testing.T, req *Request) string {
	t.Helper()
	blocks, err := renderClaudePrompt(req)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, b := range blocks {
		if s, ok := b["text"].(string); ok {
			text.WriteString(s)
		}
	}
	return text.String()
}

// A run started anew is told the result closed, and the user's words after
// it marked as the user's, not run on after the tool's last line, where the
// model read them as tool output and went on with the task (#1878).
func TestClaudePromptToldUserTextAfterAToolResultAsTheUsers(t *testing.T) {
	got := promptText(t, parsed(t, summaryRequest))
	if strings.Contains(got, "SOURCE_LAST_LINE"+summaryAsk) {
		t.Fatalf("the user's words run on after the tool's output:\n%s", got)
	}
	want := "[tool result id=call-1]\nSOURCE_LAST_LINE\n[end of tool result id=call-1]\n\n[user message]\n" + summaryAsk
	if !strings.Contains(got, want) {
		t.Fatalf("the result isn't closed before the user's words:\n%s\nwant it to hold:\n%s", got, want)
	}
	// a turn resumed in its run is told the same way
	req := parsed(t, summaryRequest)
	var turn strings.Builder
	for _, b := range renderClaudeTurn(req.Messages[1:], nil) {
		if s, ok := b["text"].(string); ok {
			turn.WriteString(s)
		}
	}
	if !strings.Contains(turn.String(), want) {
		t.Fatalf("a resumed turn isn't told the result closed:\n%s", turn.String())
	}
}

// What the tool printed stays inside its result, whatever it says: it is
// not made the user's by its words.
func TestClaudePromptKeepsInstructionsTheToolPrintedInItsResult(t *testing.T) {
	got := promptText(t, parsed(t, toolSaysRequest))
	if strings.Contains(got, "[user message]") || strings.Contains(got, "[end of tool result") {
		t.Fatalf("the tool's output was told as the user's:\n%s", got)
	}
	if !strings.Contains(got, "[tool result id=call-1]\nSOURCE_LAST_LINE\n"+summaryAsk) {
		t.Fatalf("the tool's output isn't in its result:\n%s", got)
	}
}

// continuing runs a request through serveSubscription against a run waiting
// on call-1, with a stand-in for Claude Code's input and output: it says it
// queued each user message it is written, as Claude Code 2.1.296 does
// (command_lifecycle). It returns the MCP result call-1 was answered with,
// and the user messages written, each with whether call-1 was answered
// before Claude Code had it.
func continuing(t *testing.T, body string) (mcpToolResult, []string, []bool) {
	t.Helper()
	s := New()
	b := s.subscription
	input, stdin := io.Pipe()
	stdout, output := io.Pipe()
	waiter := make(chan mcpToolResult, 1)
	run := &subscriptionRun{bridge: b, token: "tok", stdin: stdin, pending: map[string]chan mcpToolResult{"call-1": waiter},
		timer: time.AfterFunc(time.Hour, func() {})}
	b.runs[run.token], b.calls["call-1"] = run, run
	t.Cleanup(func() { b.abortAll(); stdin.Close(); input.Close(); output.Close(); stdout.Close() })
	go run.readOutput(stdout)
	type told struct {
		text     string
		answered bool
	}
	lines := make(chan told, 4)
	var answered atomic.Bool
	answer := make(chan mcpToolResult, 1)
	go func() {
		if r, ok := <-waiter; ok {
			answered.Store(true)
			answer <- r
		}
	}()
	go func() {
		sc := bufio.NewScanner(input)
		for sc.Scan() {
			var m struct {
				Type    string `json:"type"`
				UUID    string `json:"uuid"`
				Message struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil || m.Type != "user" {
				continue
			}
			// a while to read it in, as Claude Code takes: results handed
			// over meanwhile came before it
			time.Sleep(50 * time.Millisecond)
			var text strings.Builder
			for _, c := range m.Message.Content {
				text.WriteString(c.Text)
			}
			lines <- told{text.String(), answered.Load()}
			fmt.Fprintf(output, `{"type":"command_lifecycle","command_uuid":%q,"state":"queued"}`+"\n", m.UUID)
		}
	}()
	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		code, _ := s.serveSubscription(rec, httptest.NewRequest("POST", "/v1/messages", nil), provider.Anthropic, "Claude Code", "claude-sonnet-5", "", []byte(body), &Usage{},
			func(context.Context, *Request) (*subscriptionRun, <-chan Event, error) {
				return nil, nil, fmt.Errorf("started anew, not continued")
			})
		done <- code
	}()
	var got mcpToolResult
	select {
	case got = <-answer:
	case code := <-done:
		t.Fatalf("the run wasn't continued: %d", code)
	case <-time.After(5 * time.Second):
		t.Fatal("call-1 was never answered")
	}
	for _, ev := range []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Read the source's last line"}}}`,
		`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"}}}`,
		`{"type":"stream_event","event":{"type":"message_stop"}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":""}`,
	} {
		fmt.Fprintln(output, ev)
	}
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("status %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reply never ended")
	}
	var texts []string
	var early []bool
	for {
		select {
		case l := <-lines:
			texts, early = append(texts, l.text), append(early, l.answered)
		default:
			return got, texts, early
		}
	}
}

func resultText(r mcpToolResult) string {
	var text strings.Builder
	for _, c := range r.Content {
		if c["type"] == "text" {
			text.WriteString(c["text"].(string))
		}
	}
	return text.String()
}

// Results that go on in their run hand the tool's output alone over MCP;
// the user's words after it reach Claude Code as a message of the user's,
// queued before the result, so it tells them to its model as one the user
// sent, with the result (#1878). Handed over in the MCP result, they were
// the tool's output to the model.
func TestClaudeToolResultsGoOnWithTheUsersWordsAsTheirMessage(t *testing.T) {
	got, said, early := continuing(t, summaryRequest)
	if text := resultText(got); text != "SOURCE_LAST_LINE" {
		t.Fatalf("call-1 was answered %q, want the tool's output alone", text)
	}
	if len(said) != 1 || said[0] != summaryAsk {
		t.Fatalf("Claude Code was written %q, want the user's words as their message", said)
	}
	if early[0] {
		t.Fatal("call-1 was answered before Claude Code had the user's message: it goes to the model after the result's turn")
	}
}

// What the tool printed is handed over as its result, whatever it says, and
// nothing is told as the user's.
func TestClaudeToolResultsKeepInstructionsTheToolPrinted(t *testing.T) {
	got, said, _ := continuing(t, toolSaysRequest)
	if text := resultText(got); text != "SOURCE_LAST_LINE\n"+summaryAsk {
		t.Fatalf("call-1 was answered %q, want the tool's output whole", text)
	}
	if len(said) != 0 {
		t.Fatalf("the tool's output was told as the user's: %q", said)
	}
}

// A message the user sends while a tool runs still reaches the run (#872),
// as theirs.
func TestToolResultKeepsUserTextSentMidTurn(t *testing.T) {
	const mid = "<system-reminder>also answer the message I sent while the tool was running</system-reminder>"
	body := `{"model":"claude-sonnet-5","max_tokens":64,"messages":[` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"bash","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"tool finished"},{"type":"text","text":"` + mid + `"}]}]}`
	got, said, _ := continuing(t, body)
	if text := resultText(got); text != "tool finished" {
		t.Fatalf("call-1 was answered %q", text)
	}
	if len(said) != 1 || said[0] != mid {
		t.Fatalf("mid-turn user text was dropped: Claude Code was written %q", said)
	}
}
