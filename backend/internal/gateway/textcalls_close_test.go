package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// 喜欢一个人 on Discord (v0.1.1299, a Responses client, deepseek-v4.1-flash
// through a relay): the client got the text "\n</｜｜DSML｜｜ calls>",
// DeepSeek's calls wrapper closed as it writes it (no tool_, a space after
// the pipes), and nothing went on. These are the reporter's bytes: the
// close as it came, and the invokes and parameters with two pipes as their
// client's history has them.
const reportedClose = "\n</｜｜DSML｜｜ calls>"

const reportedBlock = "<｜｜DSML｜｜tool_calls>\n<｜｜DSML｜｜invoke name=\"exec_command\">\n<｜｜DSML｜｜parameter name=\"cmd\" string=\"true\">ls -la</｜｜DSML｜｜parameter>\n</｜｜DSML｜｜invoke>" + reportedClose

const execChatTool = `"tools":[{"type":"function","function":{"name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}}]`

const execResponsesTool = `"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]`

// nativeExec is a chunk in which the upstream made the call itself.
const nativeExec = `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_up","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"ls -la\"}"}}]},"finish_reason":null}]}`

// cuts are s sent as two chunks at every place a chunk's JSON can cut it.
func cuts(s string) [][]string {
	var out [][]string
	for i := 1; i < len(s); i++ {
		if utf8.ValidString(s[:i]) {
			out = append(out, []string{chunkOf(s[:i]), chunkOf(s[i:])})
		}
	}
	return out
}

// A relay that made the call of DeepSeek's invokes itself left the
// wrapper's close in the text after it: the close goes, with the line
// break before it, on every route and wherever the stream cuts it.
func TestDSMLCloseLeftAfterCalls(t *testing.T) {
	chat := `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],` + execChatTool + `}`
	for _, before := range []string{"", "<｜｜DSML｜｜ calls>\n", "<｜｜DSML｜｜tool_calls>"} {
		for _, parts := range cuts(reportedClose) {
			up := []string{nativeExec}
			if before != "" {
				up = []string{chunkOf(before), nativeExec}
			}
			text, calls, _, raw := relayedChat(t, chat, append(append(up, parts...), stopChunk)...)
			if text != "" || calls["exec_command"] != `{"cmd":"ls -la"}` || strings.Contains(raw, "DSML") {
				t.Fatalf("chat, %q before, %q: text %q calls %v\n%s", before, parts, text, calls, raw)
			}
		}
	}

	whole := `{"id":"c1","object":"chat.completion","created":1791370000,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"Running it.` + strings.ReplaceAll(reportedClose, "\n", `\n`) + `","tool_calls":[{"id":"call_up","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"ls -la\"}"}}]},"finish_reason":"tool_calls"}]}`
	text, wcalls, finish, raw := wholeChat(t, `{"model":"a/m","messages":[{"role":"user","content":"go"}],`+execChatTool+`}`, "application/json", whole)
	if strOr(text) != "Running it." || len(wcalls) != 1 || finish != "tool_calls" || strings.Contains(raw, "DSML") {
		t.Fatalf("chat whole: text %q calls %+v finish %q\n%s", strOr(text), wcalls, finish, raw)
	}

	for _, stream := range []string{"true", "false"} {
		items, raw := responsesItems(t, `{"model":"a/m","stream":`+stream+`,"input":"go",`+execResponsesTool+`}`,
			nativeExec, chunkOf(reportedClose[:6]), chunkOf(reportedClose[6:]), stopChunk)
		if len(items) != 1 || items[0]["type"] != "function_call" || items[0]["arguments"] != `{"cmd":"ls -la"}` || strings.Contains(raw, "DSML") {
			t.Fatalf("responses stream=%s: %v\n%s", stream, items, raw)
		}
	}

	fresh(t)
	a := &scripted{}
	scriptedOn(t, "a", provider.Chat, a)
	for _, stream := range []string{"true", "false"} {
		a.replies = []reply{{200, "text/event-stream", sse(nativeExec, chunkOf(reportedClose), stopChunk, `data: [DONE]`)}}
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
			`{"model":"a/m","max_tokens":100,"stream":`+stream+`,"messages":[{"role":"user","content":"go"}],"tools":[{"name":"exec_command","input_schema":{"type":"object"}}]}`)))
		body := rec.Body.String()
		if rec.Code != 200 || strings.Contains(body, "DSML") || strings.Contains(body, `"type":"text"`) ||
			!strings.Contains(body, `"tool_use"`) || !strings.Contains(body, `"stop_reason":"tool_use"`) {
			t.Fatalf("anthropic stream=%s: %d %s", stream, rec.Code, body)
		}
	}
}

// The whole block, closed as the reporter's was, is the call on every
// route, streamed at every cut and whole, opened with tool_calls, with
// " calls" or not at all.
func TestDSMLBlockClosedWithCalls(t *testing.T) {
	chat := `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],` + execChatTool + `}`
	for _, block := range []string{
		reportedBlock,
		strings.Replace(reportedBlock, "tool_calls>", " calls>", 1),
		reportedBlock[strings.Index(reportedBlock, "\n")+1:],
	} {
		s := "Let me look.\n\n" + block
		for _, parts := range cuts(s) {
			text, calls, finish, raw := relayedChat(t, chat, append(parts, stopChunk)...)
			if text != "Let me look.\n\n" || calls["exec_command"] != `{"cmd":"ls -la"}` || finish != "tool_calls" || strings.Contains(raw, "DSML") {
				t.Fatalf("chat %q: text %q calls %v finish %q\n%s", parts, text, calls, finish, raw)
			}
		}
		text, wcalls, finish, raw := wholeChat(t, `{"model":"a/m","messages":[{"role":"user","content":"go"}],`+execChatTool+`}`, "application/json", wholeChatUp(s))
		if strOr(text) != "Let me look.\n\n" || len(wcalls) != 1 || finish != "tool_calls" || strings.Contains(raw, "DSML") {
			t.Fatalf("chat whole: text %q calls %+v finish %q\n%s", strOr(text), wcalls, finish, raw)
		}
		for _, stream := range []string{"true", "false"} {
			for i := 1; i < len(s); i += 5 {
				if !utf8.ValidString(s[:i]) {
					continue
				}
				items, raw := responsesItems(t, `{"model":"a/m","stream":`+stream+`,"input":"go",`+execResponsesTool+`}`, chunkOf(s[:i]), chunkOf(s[i:]), stopChunk)
				var call map[string]any
				for _, it := range items {
					if it["type"] == "function_call" {
						call = it
					}
				}
				if call == nil || call["arguments"] != `{"cmd":"ls -la"}` || strings.Contains(raw, "DSML") {
					t.Fatalf("responses stream=%s cut %d: %v\n%s", stream, i, items, raw)
				}
			}
		}
	}
}

// A DSML close with nothing written before it and no call is dropped as
// other tags and nothing else are; text after it keeps it, as written.
func TestDSMLCloseAlone(t *testing.T) {
	chat := `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"go"}],` + execChatTool + `}`
	for _, parts := range cuts("Done." + reportedClose) {
		text, calls, finish, raw := relayedChat(t, chat, append(parts, stopChunk)...)
		if text != "Done.\n" || len(calls) != 0 || finish != "stop" || strings.Contains(raw, "DSML") {
			t.Fatalf("%q: text %q calls %v finish %q\n%s", parts, text, calls, finish, raw)
		}
	}
	text, _, finish, raw := wholeChat(t, `{"model":"a/m","messages":[{"role":"user","content":"go"}],`+execChatTool+`}`, "application/json", wholeChatUp("Done."+reportedClose))
	if strOr(text) != "Done.\n" || finish != "stop" || strings.Contains(raw, "DSML") {
		t.Fatalf("whole: text %q finish %q\n%s", strOr(text), finish, raw)
	}
	for _, stream := range []string{"true", "false"} {
		items, raw := responsesItems(t, `{"model":"a/m","stream":`+stream+`,"input":"go",`+execResponsesTool+`}`, chunkOf("Done."+reportedClose), stopChunk)
		if len(items) != 1 || items[0]["type"] != "message" || strings.Contains(raw, "DSML") {
			t.Fatalf("responses stream=%s: %v\n%s", stream, items, raw)
		}
	}
	// prose about a close: the text is kept whole
	prose := "The close is </｜DSML｜invoke> and it ends a call."
	text2, calls, finish, _ := relayedChat(t, chat, chunkOf(prose), stopChunk)
	if text2 != prose || len(calls) != 0 || finish != "stop" {
		t.Fatalf("prose: text %q calls %v finish %q", text2, calls, finish)
	}
	// and after a call the upstream made, too
	text2, _, _, _ = relayedChat(t, chat, nativeExec, chunkOf(prose), stopChunk)
	if text2 != prose {
		t.Fatalf("prose after a call: text %q", text2)
	}
}
