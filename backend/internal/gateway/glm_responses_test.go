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

// glmPlan plays a GLM Coding Plan: chat completions at /api/coding/paas/v4,
// the Responses API at /api/v1, Anthropic's messages at /api/anthropic.
type glmPlan struct {
	mu    sync.Mutex
	paths []string
	model []string
}

func (g *glmPlan) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var v struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &v)
	g.mu.Lock()
	g.paths, g.model = append(g.paths, r.URL.Path), append(g.model, v.Model)
	g.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	switch r.URL.Path {
	case "/api/v1/responses":
		io.WriteString(w, sse(
			`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"resp_1","model":"`+v.Model+`","status":"in_progress","output":[]}}`,
			`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"from responses"}`,
			`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"resp_1","model":"`+v.Model+`","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"from responses"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`))
	case "/api/coding/paas/v4/chat/completions":
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from chat"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
			`data: [DONE]`))
	case "/api/anthropic/v1/messages":
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"`+v.Model+`","usage":{"input_tokens":5}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from messages"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`data: {"type":"message_stop"}`))
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"no such route"}}`)
	}
}

// The GLM Coding Plan's Responses API (#306): the zhipu and zai presets'
// Coding Plan regions give it at /api/v1, as the plans' Codex pages set
// base_url with wire_api = "responses", so Codex's request is relayed there
// as it came rather than translated to chat; chat clients stay on the plan's
// chat completions and Claude Code on its messages.
func TestGLMCodingPlanResponses(t *testing.T) {
	for _, tc := range []struct{ preset, host string }{
		{"zhipu", "https://open.bigmodel.cn"},
		{"zai", "https://api.z.ai"},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			pr := provider.Preset(tc.preset)
			var coding, payg provider.Region
			for _, r := range pr.Regions {
				switch r.ID {
				case "coding":
					coding = r
				case "api":
					payg = r
				}
			}
			if coding.Responses != tc.host+"/api/v1" || coding.Chat != tc.host+"/api/coding/paas/v4" || coding.Anthropic != tc.host+"/api/anthropic" {
				t.Fatalf("coding plan: %+v", coding)
			}
			// pay as you go is left as it was: the docs give /api/v1 for the plan
			if payg.Responses != "" || pr.Responses != "" {
				t.Fatalf("pay as you go: %+v / %q", payg, pr.Responses)
			}

			fresh(t)
			up := &glmPlan{}
			srv := httptest.NewServer(up)
			t.Cleanup(srv.Close)
			p, err := provider.FromPreset(tc.preset)
			if err != nil {
				t.Fatal(err)
			}
			at := func(u string) string { return srv.URL + strings.TrimPrefix(u, tc.host) }
			p.Key = "glm-k"
			p.Chat, p.Responses, p.Anthropic = at(coding.Chat), at(coding.Responses), at(coding.Anthropic)
			p.Models = []string{"glm-5.3"}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			m := tc.preset + "/glm-5.3"
			for _, c := range []struct{ path, body, upstream, reply string }{
				{"/v1/responses", `{"model":"` + m + `","input":"hi","stream":true}`, "/api/v1/responses", "from responses"},
				{"/v1/chat/completions", `{"model":"` + m + `","messages":[{"role":"user","content":"hi"}],"stream":true}`, "/api/coding/paas/v4/chat/completions", "from chat"},
				{"/v1/messages", `{"model":"` + m + `","max_tokens":20,"messages":[{"role":"user","content":"hi"}],"stream":true}`, "/api/anthropic/v1/messages", "from messages"},
			} {
				up.mu.Lock()
				up.paths, up.model = nil, nil
				up.mu.Unlock()
				code, body := post(t, c.path, c.body)
				if code != 200 || !strings.Contains(body, c.reply) {
					t.Fatalf("%s: status %d: %s", c.path, code, body)
				}
				up.mu.Lock()
				paths, models := up.paths, up.model
				up.mu.Unlock()
				if len(paths) != 1 || paths[0] != c.upstream || models[0] != "glm-5.3" {
					t.Fatalf("%s asked upstream at %v %v, want %s", c.path, paths, models, c.upstream)
				}
			}
		})
	}
}
