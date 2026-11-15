package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// dashScopeRefuse answers as DashScope's compatible mode does a thinking
// model asked for one JSON body with its thinking left on.
func dashScopeRefuse(body []byte) (int, string) {
	var q struct {
		Stream         bool  `json:"stream"`
		EnableThinking *bool `json:"enable_thinking"`
	}
	json.Unmarshal(body, &q)
	if q.Stream || q.EnableThinking != nil && !*q.EnableThinking {
		return 0, ""
	}
	return 400, `{"error":{"message":"<400> InternalError.Algo.InvalidParameter: parameter.enable_thinking must be set to false for non-streaming calls","type":"invalid_request_error","param":null,"code":"invalid_parameter_error"},"id":"chatcmpl-1","request_id":"1"}`
}

// A group's classifier asks its model for one JSON body; on the Qwen
// Token Plan a model that thinks turns that away (ZekeXiao on X), so it is
// asked streamed and its answer given back whole.
func TestDashScopeWholeAskedStreamed(t *testing.T) {
	f := &fake{t: t, refuse: dashScopeRefuse, reply: sse(
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"A greeting."}}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"content":"1"}}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":5,"total_tokens":45}}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Qwen Token Plan", Key: "k", Models: []string{"m1"},
		Preset: "qwen-token-plan", Chat: up.URL + "/compatible-mode/v1"}); err != nil {
		t.Fatal(err)
	}
	srv := New()
	code, body := postTo(t, srv, "/v1/chat/completions", `{"model":"m1","stream":false,"temperature":0,"max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("%d: %s", code, body)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Choices) != 1 || out.Choices[0].Message.Content != "1" {
		t.Fatalf("answer %s (%v)", body, err)
	}
	if !strings.Contains(string(f.got), `"stream":true`) {
		t.Errorf("asked %s", f.got)
	}

	// another vendor's request for one body goes as it was asked
	f2 := &fake{t: t, reply: chatWholeOK, ctype: "application/json"}
	setup(t, provider.Chat, f2)
	code, body = postTo(t, New(), "/v1/chat/completions", `{"model":"m1","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || strings.Contains(string(f2.got), `"stream":true`) {
		t.Fatalf("%d, asked %s: %s", code, f2.got, body)
	}
}

// Both Token Plans are DashScope's (#1506): the Qwen AI platform's and
// Bailian's, by preset or by host, and the Qwen AI platform's own host.
func TestDashScopeHosts(t *testing.T) {
	for _, p := range []provider.Provider{
		{Preset: "qwen-token-plan"}, {Preset: "bailian-token-plan"},
		{Chat: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"},
		{Chat: "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1"},
		{Chat: "https://maas.qianwenaiapi.com/compatible-mode/v1"},
		{Chat: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		// Qwen Cloud's (judi0713): its presets, its plan's host, a region's
		// and the regionless Singapore one
		{Preset: "qwencloud"}, {Preset: "qwencloud-token-plan"},
		{Chat: "https://token-plan.maas.qwencloudapi.com/compatible-mode/v1"},
		{Chat: "https://us-east-1.maas.qwencloudapi.com/compatible-mode/v1"},
		{Chat: "https://maas.qwencloudapi.com/compatible-mode/v1"},
	} {
		if !dashScope(p) {
			t.Errorf("not DashScope: %+v", p)
		}
	}
	if dashScope(provider.Provider{Chat: "https://api.deepseek.com/v1"}) || dashScope(provider.Provider{Chat: "https://qianwenaiapi.com.example/v1"}) ||
		dashScope(provider.Provider{Chat: "https://maas.qwencloudapi.com.example/v1"}) {
		t.Error("another vendor is DashScope")
	}
}
