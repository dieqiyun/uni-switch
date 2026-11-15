package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Opus 5.5 turns a forced tool_choice away whatever the schema (#668:
// OmO forces its todo tool on a session's first turn). The request is asked
// again with the call left to the model and only the named tool offered,
// and that model isn't forced again; the client gets the call.
func TestForcedToolChoiceRefused(t *testing.T) {
	for _, refusal := range []string{
		// Anthropic's API and OpenRouter, for Opus 5.5, Sonnet 5.5, Fable 5.1
		`{"type":"error","error":{"type":"invalid_request_error","message":"tool_choice: type \"tool\" and \"any\" are not supported for this model."}}`,
		// the reporter's vendor, which holds the forced tool to strict mode
		`{"type":"error","error":{"type":"invalid_request_error","message":"tools.1.custom: For 'object' type, 'additionalProperties' must be explicitly set to false"}}`,
	} {
		var mu sync.Mutex
		var asked []map[string]any
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var q map[string]any
			json.Unmarshal(b, &q)
			mu.Lock()
			asked = append(asked, q)
			mu.Unlock()
			if tc, _ := q["tool_choice"].(map[string]any); tc["type"] == "tool" || tc["type"] == "any" {
				w.WriteHeader(400)
				io.WriteString(w, refusal)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(
				`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","model":"claude-opus-5-5","usage":{"input_tokens":3}}}`,
				`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"todo","input":{}}}`,
				`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"todos\":[]}"}}`,
				`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
				`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
				`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
		}))
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		if err := provider.Save(provider.Provider{ID: "ant", Name: "Ant", Key: "k", Anthropic: up.URL, Models: []string{"claude-opus-5-5"}}); err != nil {
			t.Fatal(err)
		}
		s := New()
		ask := func() *httptest.ResponseRecorder {
			body := `{"model":"ant/claude-opus-5-5","messages":[{"role":"user","content":"plan"}],
			  "tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}},
			           {"type":"function","function":{"name":"todo","parameters":{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"}}}}}}}}],
			  "tool_choice":{"type":"function","function":{"name":"todo"}}}`
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
			return rec
		}
		rec := ask()
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"todo"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if len(asked) != 2 {
			t.Fatalf("asked %d times", len(asked))
		}
		again := asked[1]
		tools, _ := again["tools"].([]any)
		if tc, _ := again["tool_choice"].(map[string]any); tc != nil && tc["type"] != "auto" || len(tools) != 1 || tools[0].(map[string]any)["name"] != "todo" {
			t.Fatalf("asked again with tool_choice %v and %d tools", again["tool_choice"], len(tools))
		}
		if rec := ask(); rec.Code != 200 || len(asked) != 3 {
			t.Fatalf("next request: %d, asked %d times: %s", rec.Code, len(asked), rec.Body)
		}
		up.Close()
	}
}
