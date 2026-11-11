package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// Claude Code sends this tool only inline, not also in the top-level tools.
// OpenCode Go's DeepSeek replies 422 "Endpoint is unavailable." to it,
// while the same definition in tools succeeds (2026-10-15).
const inlineFeedback = `{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"SendFeedback","description":"Send feedback","input_schema":{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}}}}`

func TestOpenCodeDeepSeekInlineTools(t *testing.T) {
	fresh(t)
	writeCatalog(t, `{"opencode-go":{"id":"opencode-go","models":{"deepseek-v4.1-flash":{"id":"deepseek-v4.1-flash"}}}}`)
	for _, p := range []provider.Provider{
		{ID: "go", Key: "k", Chat: "https://opencode.ai/zen/go/v1", Models: []string{"deepseek-v4.1-flash", "claude-sonnet-4-5"}},
		{ID: "other", Key: "k", Anthropic: "https://relay.test", Models: []string{"deepseek-v4.1-flash"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	var mu sync.Mutex
	var sent []string
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		mu.Lock()
		sent = append(sent, string(b))
		mu.Unlock()
		code := 200
		reply := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
		if r.URL.Host == "opencode.ai" && modelOf(b) == "deepseek-v4.1-flash" && strings.Contains(string(b), `"tool_addition"`) {
			code = 422
			reply = `{"type":"error","error":{"type":"invalid_request_error","message":"OpenCode Go: Upstream request failed: Endpoint is unavailable."}}`
		}
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})}
	for _, model := range []string{"go/deepseek-v4.1-flash", "go/claude-sonnet-4-5", "other/deepseek-v4.1-flash"} {
		t.Run(model, func(t *testing.T) {
			mu.Lock()
			sent = nil
			mu.Unlock()
			body := `{"model":"` + model + `","max_tokens":32,"messages":[{"role":"user","content":[` + inlineFeedback + `,{"type":"text","text":"Reply OK only. Do not call tools."}]}]}`
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
			if rec.Code != 200 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			mu.Lock()
			got := append([]string(nil), sent...)
			mu.Unlock()
			if len(got) != 1 {
				t.Fatalf("upstream asked %d times, want once", len(got))
			}
			if model != "go/deepseek-v4.1-flash" {
				if !strings.Contains(got[0], `"tool_addition"`) || gjson.Get(got[0], "tools").Exists() {
					t.Fatalf("an unaffected model's inline tools changed: %s", got[0])
				}
				return
			}
			if strings.Contains(got[0], `"tool_addition"`) || !reflect.DeepEqual(gjson.Get(got[0], "tools.0").Value(), gjson.Get(inlineFeedback, "tool.definition").Value()) {
				t.Errorf("inline definition was not preserved in tools: %s", got[0])
			}
			if gjson.Get(got[0], "messages.0.content.0.text").String() != "Reply OK only. Do not call tools." {
				t.Errorf("user text changed: %s", got[0])
			}
		})
	}
}

func TestDeferredInlineDefinitionPreserved(t *testing.T) {
	for _, tc := range []struct {
		name, tools, want string
	}{
		{"inline only", "", gjson.Get(inlineFeedback, "tool.definition").Raw},
		{"existing definition wins", `,"tools":[{"name":"SendFeedback","description":"current definition","input_schema":{"type":"object"}}]`, `{"name":"SendFeedback","description":"current definition","input_schema":{"type":"object"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"model":"m"` + tc.tools + `,"messages":[{"role":"user","content":[` + inlineFeedback + `]},` + ccSearchResult + `,{"role":"user","content":[` + inlineFeedback + `,{"type":"text","text":"continue"}]}]}`
			got := string(withoutDeferredTools([]byte(body)))
			if tools := gjson.Get(got, "tools").Array(); len(tools) != 1 || !reflect.DeepEqual(tools[0].Value(), gjson.Parse(tc.want).Value()) {
				t.Fatalf("tools lost or duplicated: %s", got)
			}
			if len(gjson.Get(got, "messages").Array()) != 3 || gjson.Get(got, "messages.1.content.0.content.0.tool_name").String() != "Read" || gjson.Get(got, "messages.2.content.0.text").String() != "continue" {
				t.Fatalf("conversation or ToolSearch result changed: %s", got)
			}
		})
	}
}
