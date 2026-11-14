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

// liteSubagentCatalog is models.dev's entries for the models the subagent
// is put on: OpenCode Go's DeepSeek V4.1 Flash and DeepSeek's own.
const liteSubagentCatalog = `{
  "opencode-go": {"id":"opencode-go","npm":"@ai-sdk/openai-compatible","api":"https://opencode.ai/inference/go/openai/v1","models": {
    "deepseek-v4.1-flash": {"id":"deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash","family":"deepseek-flash","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"limit":{"context":1000000,"output":384000},"provider":{"api":"https://opencode.ai/inference/go/openai/v1"}}}},
  "deepseek": {"id":"deepseek","models": {
    "deepseek-flash": {"id":"deepseek-flash","name":"DeepSeek Flash","family":"deepseek-flash","reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["high","max"]}],"tool_call":true,"interleaved":{"field":"reasoning_content"},"limit":{"context":1048576,"output":393216}}}}
}`

// TestLiteSubagentLeavesLite (TangForX on X): Codex with gpt-6.1-sol as its
// lead, which Codex's catalog marks use_responses_lite, spawns a subagent
// on OpenCode Go's DeepSeek ("Codex subagents on any model" on). Codex
// sends the subagent's turn in the lead's Lite shape, its model name alone
// changed: reasoning context "all_turns", the Lite header, its tools in an
// additional_tools item. uni-switch lifted the tools but sent the rest, and
// OpenCode Go answered 503 "Endpoint is unavailable"; the same subagent
// under a DeepSeek lead, not Lite, was answered. Every provider that isn't
// OpenAI is now sent what Codex sends a model that isn't Lite: OpenCode Go,
// DeepSeek's own API (which took the Lite shape, but has no use for it) and
// a relay the user added.
func TestLiteSubagentLeavesLite(t *testing.T) {
	fresh(t)
	writeCatalog(t, liteSubagentCatalog)
	for _, p := range []provider.Provider{
		{ID: "opencode-go", Name: "OpenCode Go", Preset: "opencode-go", Key: "k", Chat: "https://opencode.ai/zen/go/v1", Responses: "https://opencode.ai/zen/go/v1", Anthropic: "https://opencode.ai/zen/go", Catalog: "opencode-go"},
		{ID: "deepseek", Name: "DeepSeek", Preset: "deepseek", Key: "k", Chat: "https://api.deepseek.com/v1", Responses: "https://api.deepseek.com/v1", Anthropic: "https://api.deepseek.com/anthropic", Catalog: "deepseek"},
		{ID: "relay", Name: "Relay", Key: "k", Responses: "https://relay.example/v1", Models: []string{"m1"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var sent []map[string]any
	var heads []http.Header
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		var q map[string]any
		json.Unmarshal(b, &q)
		mu.Lock()
		sent, heads = append(sent, q), append(heads, r.Header.Clone())
		mu.Unlock()
		reasoning, _ := q["reasoning"].(map[string]any)
		if _, lite := reasoning["context"]; lite || r.Header.Get(liteHeader) != "" || strings.Contains(string(b), `"additional_tools"`) {
			// OpenCode Go's answer to the Lite shape, as the reporter saw it
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":{"type":"server_error","message":"Upstream request failed: Endpoint is unavailable."}}`)), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Request: r,
			Body: io.NopCloser(strings.NewReader(sse(
				`data: {"type":"response.created","response":{"id":"r1","status":"in_progress"}}`,
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`,
				`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)))}, nil
	})}
	for _, model := range []string{"opencode-go/deepseek-v4.1-flash", "deepseek/deepseek-flash", "relay/m1"} {
		t.Run(model, func(t *testing.T) {
			// the subagent's turn as Codex sends it under a Lite lead
			body := strings.Replace(codeModeRequest, `"model":"fake/m1"`, `"model":"`+model+`","store":false,"include":["reasoning.encrypted_content"]`, 1)
			req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer chatgpt-token")
			req.Header.Set("chatgpt-account-id", "acct-1")
			req.Header.Set("User-Agent", "codex_cli_rs/0.163.0 (Mac OS 26.2.0; arm64)")
			req.Header.Set("originator", "codex_cli_rs")
			req.Header.Set("session_id", "worker-1")
			req.Header.Set("x-codex-parent-thread-id", "lead-1")
			req.Header.Set("x-openai-subagent", "collab_spawn")
			req.Header.Set(liteHeader, "true")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "done") {
				t.Fatalf("status %d: %.400s", rec.Code, rec.Body)
			}
			mu.Lock()
			q, h := sent[len(sent)-1], heads[len(heads)-1]
			mu.Unlock()
			reasoning, _ := q["reasoning"].(map[string]any)
			if _, ok := reasoning["context"]; ok {
				t.Errorf("reasoning context sent: %v", reasoning)
			}
			if reasoning["effort"] == nil || reasoning["summary"] != "auto" {
				t.Errorf("the rest of reasoning lost: %v", reasoning)
			}
			if h.Get(liteHeader) != "" {
				t.Errorf("%s sent on", liteHeader)
			}
			if model == "relay/m1" && h.Get("x-codex-parent-thread-id") == "" {
				t.Errorf("Codex's other headers not sent on to the relay: %v", h)
			}
		})
	}
}

// Lite's shape is taken off only where it doesn't belong: a request that
// isn't Lite is the same bytes, and reasoning keeps its other keys.
func TestFromLite(t *testing.T) {
	plain := `{"model":"m","reasoning":{"effort":"high","summary":"auto"},"input":[{"type":"message","role":"user","content":"hi, context"}]}`
	if got := string(fromLite([]byte(plain))); got != plain {
		t.Fatalf("a request that isn't Lite changed: %s", got)
	}
	lite := `{"model":"m","reasoning":{"effort":"high","summary":"auto","context":"all_turns"},"input":[]}`
	var q struct {
		Reasoning map[string]any `json:"reasoning"`
		Input     []any          `json:"input"`
	}
	if err := json.Unmarshal(fromLite([]byte(lite)), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Reasoning) != 2 || q.Reasoning["effort"] != "high" || q.Reasoning["summary"] != "auto" || q.Input == nil {
		t.Fatalf("got %v", q)
	}
}
