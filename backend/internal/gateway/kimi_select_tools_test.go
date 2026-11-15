package gateway

import (
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Kimi Code's tool select (dynamically_loaded_tools, behind its
// KIMI_CODE_EXPERIMENTAL_TOOL_SELECT flag) keeps an MCP tool out of the
// request's tools until the model asks for it with select_tools; the tools
// it then loads go as a system message of their own in the history,
// {"role":"system","content":"","tools":[…]} (Kimi Code 2.4.1,
// llm-kimi/trait.ts convertMessage). uni-switch writes the capability on a Kimi
// Code plan model, so the tools that message loads reach Kimi callable:
// the gateway lifts them into the request's tools, as for Pi (#2322),
// each once, as Kimi Code wrote them, and the empty message goes. That
// restarts Kimi's prompt cache at each load (#4486).
func TestKimiCodeLoadedToolsReachKimi(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"kimi-for-coding","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "kimi-code-cn", Name: "Kimi Code (China)", Preset: "kimi-code-cn", Key: "k",
		Chat: up.URL + "/coding/v1", Models: []string{"kimi-for-coding"}}); err != nil {
		t.Fatal(err)
	}
	loaded := `{"type":"function","function":{"name":"mcp__docs__search","description":"Search the docs.","parameters":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}}}`
	body := `{"model":"kimi-code-cn/kimi-for-coding","stream":true,
	"tools":[{"type":"function","function":{"name":"select_tools","description":"Load tools.","parameters":{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"}}},"required":["names"]}}}],
	"messages":[
	 {"role":"system","content":"You are Kimi Code."},
	 {"role":"user","content":"search the docs for x"},
	 {"role":"assistant","content":"","tool_calls":[{"id":"select_tools:0","type":"function","function":{"name":"select_tools","arguments":"{\"names\":[\"mcp__docs__search\"]}"}}]},
	 {"role":"tool","tool_call_id":"select_tools:0","content":"Loaded mcp__docs__search."},
	 {"role":"system","content":"","tools":[` + loaded + `]},
	 {"role":"user","content":"go on"}]}`
	code, out := post(t, "/v1/chat/completions", body)
	if code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	var got struct {
		Tools    []json.RawMessage            `json:"tools"`
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(f.got, &got); err != nil {
		t.Fatalf("%v: %s", err, f.got)
	}
	var want any
	json.Unmarshal([]byte(loaded), &want)
	names := map[string]int{}
	found := false
	for _, raw := range got.Tools {
		var tool struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		json.Unmarshal(raw, &tool)
		names[tool.Function.Name]++
		var v any
		json.Unmarshal(raw, &v)
		if tool.Function.Name == "mcp__docs__search" && jsonEqual(v, want) {
			found = true
		}
	}
	if len(got.Tools) != 2 || names["select_tools"] != 1 || !found {
		t.Fatalf("tools sent: %s", f.got)
	}
	if len(got.Messages) != 5 {
		t.Fatalf("messages sent: %s", f.got)
	}
	for _, m := range got.Messages {
		if _, ok := m["tools"]; ok {
			t.Fatalf("a message still carries tools: %s", f.got)
		}
	}
}
