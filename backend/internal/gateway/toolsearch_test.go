package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Codex turn with its tool search (#258), as Codex 0.159 sends it: the
// search in the tools, and in the input a search the model made, the tools
// it found (an MCP server's, as a namespace, deferred), and a call to one.
const searchTools = `"tools":[
	{"type":"function","name":"exec_command","parameters":{"type":"object"}},
	{"type":"tool_search","execution":"client","description":"# Tool discovery\nSearches over deferred tool metadata.","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"number"}},"required":["query"],"additionalProperties":false}}
]`

const searchInput = `"input":[
	{"type":"message","role":"user","content":[{"type":"input_text","text":"make a calendar event"}]},
	{"type":"tool_search_call","call_id":"s1","execution":"client","arguments":{"query":"calendar create","limit":2}},
	{"type":"tool_search_output","call_id":"s1","status":"completed","execution":"client","tools":[
		{"type":"namespace","name":"mcp__cal","description":"Calendar","tools":[
			{"type":"function","name":"create_event","description":"Create a calendar event.","defer_loading":true,"parameters":{"type":"object","properties":{"title":{"type":"string"}}}}]}]},
	{"type":"function_call","call_id":"c1","namespace":"mcp__cal","name":"create_event","arguments":"{\"title\":\"x\"}"},
	{"type":"function_call_output","call_id":"c1","output":"created"}
]`

// A model uni-switch translates for is offered the search as a function and,
// once it has searched, the tools found; the search and its result read as
// a call and its result.
func TestToolSearchTranslated(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m",` + searchTools + `,` + searchInput + `}`))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range r.Tools {
		names = append(names, tl.Name)
	}
	if got := strings.Join(names, ","); got != "exec_command,tool_search,mcp__cal__create_event" {
		t.Fatalf("tools = %s", got)
	}
	if !strings.Contains(string(r.Tools[1].Schema), "query") || !strings.Contains(r.Tools[1].Description, "Tool discovery") {
		t.Fatalf("tool_search offered as %+v", r.Tools[1])
	}
	if !r.Namespaced["tool_search"].Search || r.Namespaced["mcp__cal__create_event"] != (nsTool{Namespace: "mcp__cal", Name: "create_event"}) {
		t.Fatalf("named = %+v", r.Namespaced)
	}
	var calls, results []Part
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			switch p.Kind {
			case ToolCall:
				calls = append(calls, p)
			case ToolResult:
				results = append(results, p)
			}
		}
	}
	if len(calls) != 2 || calls[0].Name != "tool_search" || calls[0].ID != "s1" || !strings.Contains(string(calls[0].Args), `"calendar create"`) ||
		calls[1].Name != "mcp__cal__create_event" {
		t.Fatalf("calls = %+v", calls)
	}
	if len(results) != 2 || results[0].CallID != "s1" || !strings.Contains(results[0].Text, "mcp__cal__create_event") {
		t.Fatalf("results = %+v", results)
	}
}

// The model's call to the search goes back to Codex as the
// tool_search_call Codex runs, with its arguments as an object.
func TestToolSearchCallStreamedAndRendered(t *testing.T) {
	req := &Request{Model: "m", Namespaced: map[string]nsTool{"tool_search": {Search: true}}}
	rec := httptest.NewRecorder()
	enc := encoder(provider.Responses, newSSEWriter(rec), req)
	enc.event(Event{Kind: KToolStart, ID: "s2", Name: "tool_search"})
	enc.event(Event{Kind: KToolArgs, Text: `{"query":"calendar"}`})
	enc.finish()
	var items []map[string]any
	for _, ev := range events(rec.Body.String()) {
		switch ev["type"] {
		case "response.output_item.added", "response.output_item.done":
			items = append(items, ev["item"].(map[string]any))
		case "response.function_call_arguments.done":
			t.Fatalf("function arguments for the search: %v", ev)
		}
	}
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	done := items[1]
	args, _ := done["arguments"].(map[string]any)
	if done["type"] != "tool_search_call" || done["execution"] != "client" || done["call_id"] != "s2" || args["query"] != "calendar" || done["name"] != nil {
		t.Fatalf("streamed = %v", done)
	}
	if items[0]["type"] != "tool_search_call" {
		t.Fatalf("added = %v", items[0])
	}

	res := Result{Parts: []Part{{Kind: ToolCall, ID: "s3", Name: "tool_search", Args: json.RawMessage(`{"query":"mail"}`)}}}
	var out struct {
		Output []map[string]any `json:"output"`
	}
	if err := json.Unmarshal(render(provider.Responses, res, req), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Output) != 1 || out.Output[0]["type"] != "tool_search_call" || out.Output[0]["arguments"].(map[string]any)["query"] != "mail" {
		t.Fatalf("rendered = %v", out.Output)
	}
}

// Through a Chat Completions provider, end to end: the search and the tool
// it found reach the model as functions, and its call to the search comes
// back as a tool_search_call.
func TestToolSearchThroughChat(t *testing.T) {
	f := &fake{reply: sse(
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"s9","type":"function","function":{"name":"tool_search","arguments":""}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\":\"mail\"}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`)}
	setup(t, provider.Chat, f)
	code, body := post(t, "/v1/responses", `{"model":"fake/m1","stream":true,`+searchTools+`,`+searchInput+`}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var sent struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	json.Unmarshal(f.got, &sent)
	var names []string
	for _, tl := range sent.Tools {
		names = append(names, tl.Function.Name)
	}
	if got := strings.Join(names, ","); got != "exec_command,tool_search,mcp__cal__create_event" {
		t.Fatalf("offered %s", got)
	}
	var call map[string]any
	for _, ev := range events(body) {
		if ev["type"] == "response.output_item.done" {
			call = ev["item"].(map[string]any)
		}
	}
	if call["type"] != "tool_search_call" || call["call_id"] != "s9" || call["arguments"].(map[string]any)["query"] != "mail" {
		t.Fatalf("call = %v", call)
	}
}

// Relayed to a Responses API, the search goes as a function, its call and
// result as a function's, the tools found in the tools without
// defer_loading; the model's call to it comes back a tool_search_call.
func TestToolSearchRelayed(t *testing.T) {
	f := &fake{reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"s9","name":"tool_search","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"query\":\"mail\"}"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"s9","name":"tool_search","arguments":"{\"query\":\"mail\"}","status":"completed"}}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_2","call_id":"c2","name":"exec_command","arguments":"{}","status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[{"type":"function_call","call_id":"s9","name":"tool_search","arguments":"{\"query\":\"mail\"}"}],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	code, body := post(t, "/v1/responses", `{"model":"fake/m1","stream":true,`+searchTools+`,`+searchInput+`}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	sent := string(f.got)
	for _, gone := range []string{`"tool_search_call"`, `"tool_search_output"`, `"execution"`, `"defer_loading"`, `"type":"tool_search"`} {
		if strings.Contains(sent, gone) {
			t.Errorf("relayed with %s: %s", gone, sent)
		}
	}
	var q struct {
		Tools []rTool `json:"tools"`
		Input []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Output    string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(f.got, &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Tools) != 3 || q.Tools[1].Type != "function" || q.Tools[1].Name != "tool_search" || !strings.Contains(string(q.Tools[1].Parameters), "query") ||
		q.Tools[2].Type != "namespace" || q.Tools[2].Name != "mcp__cal" || len(q.Tools[2].Tools) != 1 {
		t.Fatalf("tools = %+v", q.Tools)
	}
	if in := q.Input[1]; in.Type != "function_call" || in.CallID != "s1" || in.Name != "tool_search" || !strings.Contains(in.Arguments, "calendar create") {
		t.Fatalf("search call = %+v", in)
	}
	if in := q.Input[2]; in.Type != "function_call_output" || in.CallID != "s1" || !strings.Contains(in.Output, "mcp__cal__create_event") {
		t.Fatalf("search output = %+v", in)
	}
	var items []map[string]any
	for _, ev := range events(body) {
		if ev["type"] == "response.output_item.added" || ev["type"] == "response.output_item.done" {
			items = append(items, ev["item"].(map[string]any))
		}
		if ev["type"] == "response.completed" {
			out := ev["response"].(map[string]any)["output"].([]any)
			if out[0].(map[string]any)["type"] != "tool_search_call" {
				t.Errorf("completed output = %v", out)
			}
		}
	}
	if len(items) != 3 || items[0]["type"] != "tool_search_call" || items[1]["type"] != "tool_search_call" ||
		items[1]["arguments"].(map[string]any)["query"] != "mail" || items[1]["call_id"] != "s9" || items[1]["name"] != nil {
		t.Fatalf("items = %v", items)
	}
	if items[2]["type"] != "function_call" || items[2]["name"] != "exec_command" {
		t.Fatalf("another call changed: %v", items[2])
	}
}

// A second search in the same MCP server adds the tools the namespace
// didn't have; a request without the search is relayed as it came.
func TestSearchAsFunctionMerges(t *testing.T) {
	plain := []byte(`{"model":"m","tools":[{"type":"function","name":"a"}],"input":"hi"}`)
	if out, ok := searchAsFunction(plain); ok || string(out) != string(plain) {
		t.Fatalf("changed a request without the search: %s", out)
	}
	found := func(id, fn string) string {
		return `{"type":"tool_search_call","call_id":"` + id + `","execution":"client","arguments":{"query":"q"}},
			{"type":"tool_search_output","call_id":"` + id + `","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"mcp__cal","description":"Calendar","tools":[{"type":"function","name":"` + fn + `","defer_loading":true}]}]}`
	}
	out, ok := searchAsFunction([]byte(`{"model":"m",` + searchTools + `,"input":[` + found("s1", "create_event") + `,` + found("s2", "list_events") + `,` + found("s3", "create_event") + `]}`))
	if !ok {
		t.Fatal("not changed")
	}
	var q struct {
		Tools []rTool `json:"tools"`
	}
	json.Unmarshal(out, &q)
	if len(q.Tools) != 3 || len(q.Tools[2].Tools) != 2 || q.Tools[2].Tools[0].Name != "create_event" || q.Tools[2].Tools[1].Name != "list_events" {
		t.Fatalf("tools = %+v", q.Tools)
	}
}

// Split anywhere, the stream's lines come out whole and mended.
func TestSearchTidySplit(t *testing.T) {
	in := sse(
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"s1","name":"tool_search","arguments":"{\"query\":\"x\"}"}}`,
		`data: {"type":"response.output_text.delta","delta":"tool_search <b>"}`)
	for cut := 1; cut < len(in); cut += 7 {
		var tidy searchTidy
		got := string(tidy.write([]byte(in[:cut]))) + string(tidy.write([]byte(in[cut:]))) + string(tidy.flush())
		if !strings.Contains(got, `"type":"tool_search_call"`) || strings.Contains(got, `"function_call"`) ||
			!strings.Contains(got, `"delta":"tool_search <b>"`) || !strings.HasPrefix(got, "event: response.output_item.done\n\n") {
			t.Fatalf("cut at %d: %s", cut, got)
		}
	}
}
