package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// zcodeAsking is jinyue806's reproduction of #2622 as they sent it to
// uni-switch, with tool standing in for its tools[0]. The tools are the
// shapes the report tried, each of which SenseNova turned away.
const zcodeAsking = `{
    "model": "sensenova/kimi-k3",
    "messages": [{"role":"user","content":"hi"}],
    "max_tokens": 8,
    "tools": [TOOL]
  }`

var zcodeTools = map[string]string{
	"name and parameters": `{"name":"f","parameters":{"type":"object","properties":{}}}`,
	"mcp inputSchema":     `{"name":"f","inputSchema":{"type":"object","properties":{}}}`,
	"with description":    `{"name":"f","description":"d","parameters":{"type":"object","properties":{}}}`,
	"flat function":       `{"type":"function","name":"f","parameters":{"type":"object","properties":{}}}`,
}

// A tool a Chat client sends in MCP's or another API's shape reaches the
// upstream as a Chat function tool when relayed, and as the named tool
// when translated (#2622).
func TestChatToolInAnotherShapeIsShaped(t *testing.T) {
	srv, seen := relayOfEvery(t)
	for _, route := range []struct {
		name, model, path string
		p                 provider.Provider
		check             func(t *testing.T, body string)
	}{
		{"chat relayed", "kimi-k3", "/v1/chat/completions", provider.Provider{Chat: srv.URL + "/v1"}, func(t *testing.T, body string) {
			tool := gjson.Get(body, "tools.0")
			if tool.Get("type").String() != "function" || tool.Get("function.name").String() != "f" ||
				tool.Get("function.parameters.type").String() != "object" {
				t.Errorf("tool relayed as %s", tool.Raw)
			}
			for _, k := range []string{"name", "parameters", "inputSchema", "description"} {
				if tool.Get(k).Exists() {
					t.Errorf("%s left beside the function: %s", k, tool.Raw)
				}
			}
		}},
		{"messages", "claude-opus-5-5", "/v1/messages", provider.Provider{Anthropic: srv.URL}, func(t *testing.T, body string) {
			if tool := gjson.Get(body, "tools.0"); tool.Get("name").String() != "f" || tool.Get("input_schema.type").String() != "object" {
				t.Errorf("tool sent as %s", tool.Raw)
			}
		}},
		{"responses", "kimi-k3", "/v1/responses", provider.Provider{Responses: srv.URL + "/v1"}, func(t *testing.T, body string) {
			if tool := gjson.Get(body, "tools.0"); tool.Get("name").String() != "f" || tool.Get("parameters.type").String() != "object" {
				t.Errorf("tool sent as %s", tool.Raw)
			}
		}},
	} {
		for shape, tool := range zcodeTools {
			t.Run(route.name+"/"+shape, func(t *testing.T) {
				fresh(t)
				p := route.p
				p.ID, p.Name, p.Key, p.Models = "sensenova", "SenseNova", "k", []string{route.model}
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
				seen()
				body := strings.Replace(strings.Replace(zcodeAsking, "TOOL", tool, 1), "kimi-k3", route.model, 1)
				if code, b := post(t, "/v1/chat/completions", body); code != 200 {
					t.Fatalf("%d %s", code, b)
				}
				paths, bodies := seen()
				if len(paths) != 1 || paths[0] != route.path {
					t.Fatalf("went to %v, want %s", paths, route.path)
				}
				route.check(t, bodies[0])
			})
		}
	}
}

// A request whose tools are all in the Chat shape, or hosted, is relayed
// byte for byte; only a tool in another shape is rewritten, and its other
// fields stay.
func TestShapeChatToolsLeavesChatShape(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}]}`,
		`{"model":"m","messages":[],"tools":[{"type":"web_search_preview"},{"type":"function","function":{"name":"g"}}]}`,
		`{"model":"m","messages":[],"tools":[{"type":"custom","name":"apply_patch"}]}`,
		`{"model":"m","messages":[],"tools":[{"description":"no name"}]}`,
		`{"model":"m","messages":[{"role":"user","content":"tools"}]}`,
	} {
		if got := string(shapeChatTools([]byte(body))); got != body {
			t.Errorf("%s\nbecame %s", body, got)
		}
	}
	got := shapeChatTools([]byte(`{"model":"m","tools":[{"type":"function","function":{"name":"a"}},{"name":"b","input_schema":{"type":"object"},"strict":true,"cache_control":{"type":"ephemeral"}}]}`))
	if a := gjson.GetBytes(got, "tools.0").Raw; a != `{"type":"function","function":{"name":"a"}}` {
		t.Errorf("a Chat tool changed: %s", a)
	}
	b := gjson.GetBytes(got, "tools.1")
	if b.Get("function.name").String() != "b" || !b.Get("function.strict").Bool() ||
		b.Get("function.parameters.type").String() != "object" || b.Get("cache_control.type").String() != "ephemeral" {
		t.Errorf("tool shaped as %s", b.Raw)
	}
}
