package provider

import (
	"strings"
	"testing"
)

// A Bedrock key signs the Anthropic endpoint with x-api-key alone, since
// Bedrock turns away a request that also carries a Bearer (#176), and chat
// completions with a Bearer.
func TestBedrockAuth(t *testing.T) {
	p, _ := legacyVendor("bedrock")
	p.Key = "ABSK-key"
	if h := AuthHeaders(p, Anthropic); h["x-api-key"] != "ABSK-key" || len(h) != 1 {
		t.Fatalf("anthropic: %v", h)
	}
	if h := AuthHeaders(p, Chat); h["Authorization"] != "Bearer ABSK-key" {
		t.Fatalf("chat: %v", h)
	}
	if h := AuthHeaders(p, Responses); h["Authorization"] != "Bearer ABSK-key" {
		t.Fatalf("responses: %v", h)
	}
}

// Bedrock's GPT models turn max_tokens away (#176): its chat test asks
// the reply's length as max_completion_tokens.
func TestBedrockTestAsksCompletionTokens(t *testing.T) {
	p, err := legacyVendor("bedrock")
	if err != nil {
		t.Fatal(err)
	}
	_, body := tinyBody(p, Chat, "global.openai.gpt-6-sol")
	if !strings.Contains(body, `"max_completion_tokens":16`) || strings.Contains(body, `"max_tokens"`) {
		t.Fatalf("body: %s", body)
	}
	_, body = tinyBody(p, Anthropic, "global.anthropic.claude-opus-5-5")
	if !strings.Contains(body, `"max_tokens":16`) {
		t.Fatalf("anthropic body: %s", body)
	}
}
