package gateway

import (
	"encoding/json"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestWithMaxOutputAnthropicThinking(t *testing.T) {
	out := withMaxOutput(provider.Anthropic, []byte(`{"max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":16000}}`), 8192)
	var v struct {
		MaxTokens int `json:"max_tokens"`
		Thinking  struct {
			Type   string `json:"type"`
			Budget int    `json:"budget_tokens"`
		} `json:"thinking"`
	}
	json.Unmarshal(out, &v)
	if v.MaxTokens != 8192 || v.Thinking.Type != "enabled" || v.Thinking.Budget != 8191 {
		t.Fatalf("%s", out)
	}
	out = withMaxOutput(provider.Anthropic, []byte(`{"max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":16000}}`), 1000)
	json.Unmarshal(out, &v)
	if v.MaxTokens != 1000 || v.Thinking.Type != "disabled" {
		t.Fatalf("%s", out)
	}
	if out := withMaxOutput(provider.Responses, []byte(`{"max_output_tokens":100}`), 8192); string(out) != `{"max_output_tokens":100}` {
		t.Fatalf("%s", out)
	}
}
