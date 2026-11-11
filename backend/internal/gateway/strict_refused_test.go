package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// strictRefusal is #1980's refusal ("Unsupported Chat Completions API
// field: tools[0].***.strict", the segment masked in the report), in
// OpenAI's error shape; strictRefusalResponses is the same validator's on
// Responses, where a tool's strict sits at tools[0].strict.
const (
	strictRefusal          = `{"error":{"message":"Unsupported Chat Completions API field: tools[0].function.strict","type":"invalid_request_error"}}`
	strictRefusalResponses = `{"error":{"message":"Unsupported Responses API field: tools[0].strict","type":"invalid_request_error"}}`
)

// toolsHaveStrict reports whether any function tool of a Chat or Responses
// body carries strict.
func toolsHaveStrict(q map[string]any) bool {
	tools, _ := q["tools"].([]any)
	for _, t := range tools {
		tm, _ := t.(map[string]any)
		if fn, ok := tm["function"].(map[string]any); ok {
			tm = fn
		}
		if _, ok := tm["strict"]; ok {
			return true
		}
	}
	return false
}

// OpenCode 2.0.25 offers every tool with "strict": false, on Chat through
// @ai-sdk/openai-compatible (uni-switch's provider for it) and on Responses
// through @ai-sdk/openai (the fixtures are its requests, recorded against
// a fake upstream). An upstream that takes no strict on Chat turned every
// turn away (#1980), and one may on Responses too. It is asked again
// without it, and the next turn goes without it at once; an upstream that
// takes it still gets it.
func TestToolStrictLeftOutWhereRefused(t *testing.T) {
	for name, c := range map[string]struct{ fixture, path, refusal string }{
		"OpenCode on Chat":      {"testdata/opencode2_chat_tools.json", "/v1/chat/completions", strictRefusal},
		"OpenCode on Responses": {"testdata/opencode2_responses_tools.json", "/v1/responses", strictRefusalResponses},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(c.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var q map[string]any
			if err := json.Unmarshal(raw, &q); err != nil {
				t.Fatal(err)
			}
			if !toolsHaveStrict(q) {
				t.Fatal("fixture has no strict")
			}
			fresh(t)
			var mu sync.Mutex
			var refusing, taking []map[string]any
			strict := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				var q map[string]any
				json.Unmarshal(b, &q)
				mu.Lock()
				refusing = append(refusing, q)
				mu.Unlock()
				if toolsHaveStrict(q) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(400)
					io.WriteString(w, c.refusal)
					return
				}
				answer(w, r)
			}))
			t.Cleanup(strict.Close)
			lax := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				var q map[string]any
				json.Unmarshal(b, &q)
				mu.Lock()
				taking = append(taking, q)
				mu.Unlock()
				answer(w, r)
			}))
			t.Cleanup(lax.Close)
			for _, p := range []provider.Provider{
				{ID: "custom-provider", Name: "custom-provider", Key: "k", Models: []string{"m1"}},
				{ID: "lax", Name: "Lax", Key: "k", Models: []string{"m1"}},
			} {
				url := strict.URL + "/v1"
				if p.ID == "lax" {
					url = lax.URL + "/v1"
				}
				if c.path == "/v1/responses" {
					p.Responses = url
				} else {
					p.Chat = url
				}
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			s := New()
			ask := func(model string) *httptest.ResponseRecorder {
				q["model"] = model
				b, _ := json.Marshal(q)
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", c.path, strings.NewReader(string(b))))
				return rec
			}
			rec := ask("custom-provider/m1")
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ok") {
				t.Fatalf("custom-provider: %d %s", rec.Code, rec.Body)
			}
			if len(refusing) != 2 || !toolsHaveStrict(refusing[0]) || toolsHaveStrict(refusing[1]) {
				t.Fatalf("custom-provider was asked %d times", len(refusing))
			}
			if tools, _ := refusing[1]["tools"].([]any); len(tools) != 12 {
				t.Fatalf("asked again with %d tools", len(tools))
			}
			if rec = ask("custom-provider/m1"); rec.Code != 200 || len(refusing) != 3 || toolsHaveStrict(refusing[2]) {
				t.Fatalf("next turn: %d, asked %d times", rec.Code, len(refusing))
			}
			if rec = ask("lax/m1"); rec.Code != 200 || len(taking) != 1 || !toolsHaveStrict(taking[0]) {
				t.Fatalf("lax: %d, asked %d times", rec.Code, len(taking))
			}
		})
	}
}

// A refusal of a tool's strict is known however the path is written; a
// 400 over strict's value or over a schema strict mode can't take is not
// taken for one.
func TestToolStrictRefusalWordings(t *testing.T) {
	for _, body := range []string{
		strictRefusal,
		"Unsupported Chat Completions API field: tools[0].***.strict",
		`{"error":{"message":"Unknown parameter: 'tools[3].function.strict'.","type":"invalid_request_error","param":"tools[3].function.strict","code":"unknown_parameter"}}`,
		`{"error":{"message":"tools.0.function.strict: Extra inputs are not permitted"}}`,
		`{"detail":[{"type":"extra_forbidden","loc":["body","tools",0,"function","strict"],"msg":"Extra inputs are not permitted","input":false}]}`,
		`{"error":{"message":"Unrecognized request argument supplied: tools[0].strict"}}`,
	} {
		if !toolStrictRefusal.MatchString(body) {
			t.Errorf("not a strict refusal: %s", body)
		}
	}
	for _, body := range []string{
		`{"error":{"message":"Invalid schema for function 'read': In context=(), 'additionalProperties' is required to be supplied and to be false.","type":"invalid_request_error","param":"tools[0].function.parameters","code":"invalid_function_parameters"}}`,
		`{"error":{"message":"Invalid type for 'tools[0].function.strict': expected a boolean, but got a string instead.","type":"invalid_request_error","param":"tools[0].function.strict","code":"invalid_type"}}`,
		`{"error":{"message":"Unsupported parameter: 'store'.","type":"invalid_request_error"}}`,
	} {
		if toolStrictRefusal.MatchString(body) {
			t.Errorf("taken for a strict refusal: %s", body)
		}
	}
}
