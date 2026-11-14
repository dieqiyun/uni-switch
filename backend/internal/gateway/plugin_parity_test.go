package gateway

import (
	"encoding/json"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// thoughtChat is a fast request with an assistant turn that reasoned, and
// chatBody the chat request built of it for host.
var thoughtChat = &Request{Fast: true, Messages: []Message{
	{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}},
	{Role: "assistant", Parts: []Part{{Kind: Thinking, Text: "pondered"}, {Kind: Text, Text: "hello"}}},
	{Role: "user", Parts: []Part{{Kind: Text, Text: "again"}}},
}}

func chatBody(t *testing.T, host string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buildChat(thoughtChat, "m", host, false), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Cursor moved onto its plugin is told fast mode as the built-in told
// Cursor: its plugin reads the chat request's service_tier. No other
// chat upstream is told it.
func TestCursorPluginFast(t *testing.T) {
	if m := chatBody(t, "cursor"); m["service_tier"] != "priority" {
		t.Errorf("Cursor's plugin wasn't told fast: %v", m["service_tier"])
	}
	for _, host := range []string{"api.openai.com", "openrouter.ai", "zed", "api.deepseek.com", provider.CommandCodePlanID} {
		if m := chatBody(t, host); m["service_tier"] != nil {
			t.Errorf("%s was told a service tier", host)
		}
	}
}

// A Command Code Go conversation moved onto its plugin keeps its
// reasoning, as the built-in replayed it to /alpha/generate: its plugin
// reads reasoning_content. DeepSeek and Qoder's plugin, which sends it on
// as Qoder's client does, get it too; others don't.
func TestCommandCodePluginReasoning(t *testing.T) {
	reasoning := func(host string) any {
		return chatBody(t, host)["messages"].([]any)[1].(map[string]any)["reasoning_content"]
	}
	for _, host := range []string{provider.CommandCodePlanID, "api.deepseek.com", "qoder", "qoder-cn"} {
		if got := reasoning(host); got != "pondered" {
			t.Errorf("%s got reasoning %v", host, got)
		}
	}
	for _, host := range []string{"api.openai.com", "cursor", "zed"} {
		if got := reasoning(host); got != nil {
			t.Errorf("%s got reasoning_content %v", host, got)
		}
	}
}

// A moved provider's account is told in the trace as its provider's, so
// its plan reads as the built-in's did, not as a plugin's.
func TestTraceNamesMovedAccount(t *testing.T) {
	p := provider.Provider{ID: "grok", Name: "Grok", Account: &provider.Account{Agent: "plugin", User: "me@example.com", Plan: "SuperGrok Heavy"}}
	w := weighed(candidate{p: p, rest: "grok", model: "grok-4"}, p, weighing{}, false, "")
	if w.Kind != "account" || w.Agent != "grok" || w.Plan != "SuperGrok Heavy" {
		t.Fatalf("weighed = %+v, want grok's account", w)
	}
}

// A failed tool result reaches the plugins of Devin and Qoder as their
// built-ins sent it, "Error: " first, and Cursor's as is_error, which its
// plugin reads; no other chat upstream is told either.
func TestPluginToolFailureSaid(t *testing.T) {
	failed := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "go"}}},
		{Role: "assistant", Parts: []Part{{Kind: ToolCall, ID: "c1", Name: "Bash", Args: json.RawMessage(`{}`)}}},
		{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: "c1", Text: "exit status 1", IsError: true}}},
	}}
	tool := func(host string) map[string]any {
		var m struct{ Messages []map[string]any }
		if err := json.Unmarshal(buildChat(failed, "m", host, false), &m); err != nil {
			t.Fatal(err)
		}
		for _, msg := range m.Messages {
			if msg["role"] == "tool" {
				return msg
			}
		}
		t.Fatalf("%s: no tool message in %v", host, m.Messages)
		return nil
	}
	for _, host := range []string{"devin", "qoder", "qoder-cn"} {
		if got := tool(host); got["content"] != "Error: exit status 1" || got["is_error"] != nil {
			t.Errorf("%s: %v", host, got)
		}
	}
	if got := tool("cursor"); got["content"] != "exit status 1" || got["is_error"] != true {
		t.Errorf("cursor: %v", got)
	}
	for _, host := range []string{"api.openai.com", "api.deepseek.com", "zed", provider.CommandCodePlanID} {
		if got := tool(host); got["content"] != "exit status 1" || got["is_error"] != nil {
			t.Errorf("%s: %v", host, got)
		}
	}
}
