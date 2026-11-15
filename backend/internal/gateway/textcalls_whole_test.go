package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// issueDSML is #1845's reply: DeepSeek's DSML for a call of the request's
// Bash tool, as cline-pass/deepseek-v4.1-flash wrote it into the content
// of a reply asked for whole. The issue shows its brackets full-width
// (＜｜DSML｜tool_calls＞) to keep them from rendering; the same request
// streamed was rescued, so the bytes had DeepSeek's own <｜DSML｜.
const issueDSML = "<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"Bash\">\n<｜DSML｜parameter name=\"command\" string=\"true\">ls /tmp</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>"

const issueTools = `"tools":[{"type":"function","function":{"name":"Bash","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}}]`

// wholeChatUp is a chat.completion whose message says content.
func wholeChatUp(content string) string {
	c, _ := json.Marshal(content)
	return `{"id":"c1","object":"chat.completion","created":1791370000,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":` + string(c) + `},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
}

type wholeCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// wholeChat sends a Chat client's request for one body (body) through
// the gateway to an upstream that answers up whole under ctype, and gives
// back the message's content (nil for null), its calls, the finish reason
// and the bytes the client read.
func wholeChat(t *testing.T, body, ctype, up string) (text *string, calls []wholeCall, finish, raw string) {
	t.Helper()
	fresh(t)
	scriptedOn(t, "a", provider.Chat, &scripted{replies: []reply{{200, ctype, up}}})
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	raw = rec.Body.String()
	var got struct {
		Choices []struct {
			Message struct {
				Content   *string     `json:"content"`
				ToolCalls []wholeCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got.Choices) != 1 {
		t.Fatalf("%d %s", rec.Code, raw)
	}
	m := got.Choices[0].Message
	return m.Content, m.ToolCalls, got.Choices[0].FinishReason, raw
}

func strOr(s *string) string {
	if s == nil {
		return "<null>"
	}
	return *s
}

// #1845: a call a model wrote into the text of a Chat reply asked for
// whole (stream:false) reached the client as that text, finish_reason
// stop, and the agent ran nothing; the same request streamed was rescued.
// It is the call it is now, in every format the stream reads, the text
// before it kept and the markup gone.
func TestTextWrittenCallWhole(t *testing.T) {
	ask := `{"model":"a/m","stream":false,"messages":[{"role":"system","content":"You are a coding agent. Use the Bash tool."},{"role":"user","content":"ls /tmp"}],` + issueTools + `}`
	for _, c := range []struct {
		name, ctype, up string
		text            string // "<null>" for none
		args            string
	}{
		{"the issue's DSML", "application/json", wholeChatUp(issueDSML), "<null>", `{"command":"ls /tmp"}`},
		{"DSML after text", "application/json; charset=utf-8", wholeChatUp("看一下。\n\n" + issueDSML), "看一下。\n\n", `{"command":"ls /tmp"}`},
		// a Go relay escapes < and > in its JSON
		{"escaped brackets", "application/json", strings.NewReplacer("<", `<`, ">", `>`).Replace(wholeChatUp(issueDSML)), "<null>", `{"command":"ls /tmp"}`},
		// a whole reply under text/plain, which eventStream found isn't
		// a stream
		{"text/plain", "text/plain; charset=utf-8", wholeChatUp(issueDSML), "<null>", `{"command":"ls /tmp"}`},
		{"Hermes", "application/json", wholeChatUp("我先看看目录。\n\n<tool_call>{\"name\":\"Bash\",\"arguments\":{\"command\":\"ls -la\"}}</tool_call>"), "我先看看目录。\n\n", `{"command":"ls -la"}`},
		// #823's: a raw newline in the JSON's string, DeepSeek's closes strewn after it
		{"Hermes with junk", "application/json", wholeChatUp("<tool_call>{\"name\":\"Bash\",\"arguments\":{\"command\":\"cd /e && for s in a b; do\n\techo \\\"$s\\\"; done\"}}</｜DSML｜parameter>\n</｜DSML｜invoke"), "<null>", `{"command":"cd /e && for s in a b; do\n\techo \"$s\"; done"}`},
		// #917's: half Hermes, half DeepSeek's template, cut at its end
		{"param", "application/json", wholeChatUp("Let me run it.\n\n<tool_call>{\"name\":\"Bash\">\n<parameter name=\"command\">cd /e/Project/synara && node .review-tmp\n"), "Let me run it.\n\n", `{"command":"cd /e/Project/synara && node .review-tmp"}`},
		// #906's: GLM's own
		{"GLM", "application/json", wholeChatUp("Looking.<tool_call>Bash<arg_key>command</arg_key><arg_value>ls -la</arg_value></tool_call>"), "Looking.", `{"command":"ls -la"}`},
		// #823's: a <tool_call> wrapping DeepSeek's own invoke
		{"wrapped invoke", "application/json", wholeChatUp("看一下。<tool_call>\n<｜DSML｜invoke name=\"Bash\">\n<｜DSML｜parameter name=\"command\" string=\"true\">ls</｜DSML｜parameter>\n</｜DSML｜invoke>\n</tool_call>"), "看一下。", `{"command":"ls"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, calls, finish, raw := wholeChat(t, ask, c.ctype, c.up)
			var want, got any
			json.Unmarshal([]byte(c.args), &want)
			if len(calls) == 1 {
				json.Unmarshal([]byte(calls[0].Function.Arguments), &got)
			}
			if strOr(text) != c.text || len(calls) != 1 || calls[0].Function.Name != "Bash" || calls[0].Type != "function" ||
				calls[0].ID == "" || got == nil || !jsonEqual(got, want) || finish != "tool_calls" {
				t.Fatalf("text %q calls %+v finish %q\n%s", strOr(text), calls, finish, raw)
			}
			if strings.Contains(raw, "DSML") || strings.Contains(raw, "tool_call>") || strings.Contains(raw, "arg_key") || strings.Contains(raw, "parameter") {
				t.Fatalf("markup left: %s", raw)
			}
			if !strings.Contains(raw, `"total_tokens":5`) || !strings.Contains(raw, `"id":"c1"`) {
				t.Fatalf("the reply's own fields lost: %s", raw)
			}
		})
	}

	t.Run("two calls", func(t *testing.T) {
		two := issueDSML + "\n" + strings.ReplaceAll(issueDSML, "ls /tmp", "pwd")
		text, calls, finish, raw := wholeChat(t, ask, "application/json", wholeChatUp(two))
		if text != nil || len(calls) != 2 || calls[0].Function.Arguments != `{"command":"ls /tmp"}` || calls[1].Function.Arguments != `{"command":"pwd"}` ||
			calls[0].ID == calls[1].ID || finish != "tool_calls" {
			t.Fatalf("text %q calls %+v finish %q\n%s", strOr(text), calls, finish, raw)
		}
	})

	// what a stream leaves as text, a whole reply leaves too
	for _, c := range []struct{ name, body, content string }{
		{"no tools offered", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`, issueDSML},
		{"a tool not offered", ask, `Like this: <tool_call>{"name":"rm_rf","arguments":{}}</tool_call>`},
		{"cut short", ask, `<tool_call>{"name":"Bash","arguments":{"command":"l`},
		{"lookalike", ask, "a <tooltip> b </tool_call> c"},
		{"empty", ask, ""},
		{"plain", ask, "Nothing to run."},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, calls, finish, raw := wholeChat(t, c.body, "application/json", wholeChatUp(c.content))
			if strOr(text) != c.content || len(calls) != 0 || finish != "stop" {
				t.Fatalf("text %q calls %+v finish %q\n%s", strOr(text), calls, finish, raw)
			}
		})
	}

	// tags and nothing else go, as a stream drops them (#823)
	t.Run("tags only", func(t *testing.T) {
		text, calls, finish, raw := wholeChat(t, ask, "application/json", wholeChatUp("<tool_call>\n</｜DSML｜parameter>\n</｜DSML｜invoke>\n</"))
		if strOr(text) != "" || len(calls) != 0 || finish != "stop" || strings.Contains(raw, "DSML") {
			t.Fatalf("text %q calls %+v finish %q\n%s", strOr(text), calls, finish, raw)
		}
	})

	// a call the model made itself keeps the reply as it came
	t.Run("beside a native call", func(t *testing.T) {
		c, _ := json.Marshal(issueDSML)
		up := `{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":` + string(c) + `,"tool_calls":[{"id":"call_9","type":"function","function":{"name":"Bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`
		text, calls, finish, raw := wholeChat(t, ask, "application/json", up)
		if strOr(text) != issueDSML || len(calls) != 1 || calls[0].ID != "call_9" || finish != "tool_calls" {
			t.Fatalf("text %q calls %+v finish %q\n%s", strOr(text), calls, finish, raw)
		}
	})

	// a body cut short, or none, goes on as it came: nothing is made up
	t.Run("truncated body", func(t *testing.T) {
		fresh(t)
		up := wholeChatUp(issueDSML)
		up = up[:strings.Index(up, "ls /tmp")+len("ls /tmp")]
		scriptedOn(t, "a", provider.Chat, &scripted{replies: []reply{{200, "application/json", up}}})
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(ask)))
		if rec.Code == 200 && rec.Body.String() != up || strings.Contains(rec.Body.String(), `"tool_calls"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("empty body", func(t *testing.T) {
		fresh(t)
		scriptedOn(t, "a", provider.Chat, &scripted{replies: []reply{{200, "application/json", ""}}})
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(ask)))
		if rec.Code == 200 && rec.Body.Len() != 0 || strings.Contains(rec.Body.String(), `"tool_calls"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
}

// The issue's DSML through each translation of a Chat upstream, which
// uni-switch always asks streamed: a Responses or Anthropic client, streamed
// or whole, and a Chat client's request for one body that is asked
// streamed (DashScope's thinking models), all get the call.
func TestTextWrittenCallWholeTranslated(t *testing.T) {
	up := []string{chunkOf("Running it.\n\n" + issueDSML[:40]), chunkOf(issueDSML[40:]), stopChunk}
	for _, stream := range []string{"true", "false"} {
		items, raw := responsesItems(t, `{"model":"a/m","stream":`+stream+`,"input":"ls /tmp","tools":[{"type":"function","name":"Bash","parameters":{"type":"object"}}]}`, up...)
		var call map[string]any
		for _, it := range items {
			if it["type"] == "function_call" {
				call = it
			}
		}
		if call == nil || call["name"] != "Bash" || call["arguments"] != `{"command":"ls /tmp"}` || strings.Contains(raw, "DSML") || !strings.Contains(raw, "Running it.") {
			t.Fatalf("responses stream=%s: %v\n%s", stream, call, raw)
		}

		fresh(t)
		scriptedOn(t, "a", provider.Chat, &scripted{replies: []reply{{200, "text/event-stream", sse(append(up, `data: [DONE]`)...)}}})
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
			`{"model":"a/m","max_tokens":100,"stream":`+stream+`,"messages":[{"role":"user","content":"ls /tmp"}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`)))
		body := rec.Body.String()
		if rec.Code != 200 || strings.Contains(body, "DSML") || !strings.Contains(body, `"tool_use"`) || !strings.Contains(body, `"stop_reason":"tool_use"`) {
			t.Fatalf("messages stream=%s: %d %s", stream, rec.Code, body)
		}
	}

	f := &fake{t: t, refuse: dashScopeRefuse, reply: sse(append(up, `data: [DONE]`)...)}
	srv := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Qwen Token Plan", Key: "k", Models: []string{"m1"},
		Preset: "qwen-token-plan", Chat: srv.URL + "/compatible-mode/v1"}); err != nil {
		t.Fatal(err)
	}
	code, body := postTo(t, New(), "/v1/chat/completions", `{"model":"m1","stream":false,"messages":[{"role":"user","content":"ls /tmp"}],`+issueTools+`}`)
	var out struct {
		Choices []struct {
			Message struct {
				Content   *string     `json:"content"`
				ToolCalls []wholeCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &out) != nil || len(out.Choices) != 1 || !strings.Contains(string(f.got), `"stream":true`) {
		t.Fatalf("%d %s (asked %s)", code, body, f.got)
	}
	m := out.Choices[0]
	if len(m.Message.ToolCalls) != 1 || m.Message.ToolCalls[0].Function.Arguments != `{"command":"ls /tmp"}` || m.FinishReason != "tool_calls" ||
		strings.Contains(body, "DSML") || !strings.Contains(strOr(m.Message.Content), "Running it.") {
		t.Fatalf("asked streamed, given whole: %s", body)
	}
}
