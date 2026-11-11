package provider

import (
	"testing"
)

// A provider added by its address at a Mantle host is Mantle's: Bedrock's
// quirks, and the APIs it wasn't given filled in.
func TestBedrockMantleByAddress(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		in                   Provider
		chat, resp, messages string
	}{
		{"chat at /v1", Provider{ID: "m", Chat: "https://bedrock-mantle.eu-west-1.api.aws/v1"},
			"https://bedrock-mantle.eu-west-1.api.aws/v1", "https://bedrock-mantle.eu-west-1.api.aws/v1", "https://bedrock-mantle.eu-west-1.api.aws/anthropic"},
		{"chat at /openai/v1", Provider{ID: "m", Chat: "https://bedrock-mantle.eu-west-1.api.aws/openai/v1/"},
			"https://bedrock-mantle.eu-west-1.api.aws/v1", "https://bedrock-mantle.eu-west-1.api.aws/v1", "https://bedrock-mantle.eu-west-1.api.aws/anthropic"},
		{"messages alone", Provider{ID: "m", Anthropic: "https://bedrock-mantle.us-west-2.api.aws/anthropic"},
			"https://bedrock-mantle.us-west-2.api.aws/v1", "https://bedrock-mantle.us-west-2.api.aws/v1", "https://bedrock-mantle.us-west-2.api.aws/anthropic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := normalize(tc.in)
			if !p.IsBedrock() || !p.IsBedrockMantle() {
				t.Fatalf("not taken for Mantle: %+v", p)
			}
			if p.Chat != tc.chat || p.Responses != tc.resp || p.Anthropic != tc.messages {
				t.Fatalf("endpoints: %q %q %q", p.Chat, p.Responses, p.Anthropic)
			}
		})
	}
	for _, other := range []Provider{
		{ID: "o", Chat: "https://bedrock-mantle.example.com/v1"},
		{ID: "o", Chat: "https://api.aws/v1"},
		{ID: "o", Chat: "https://relay.example.com/bedrock-mantle.us-east-1.api.aws/v1"},
	} {
		if p := normalize(other); p.IsBedrockMantle() || p.IsBedrock() || p.Anthropic != "" {
			t.Errorf("%s taken for Mantle: %+v", other.Chat, p)
		}
	}
	// another preset at a Mantle address keeps its own APIs
	if p := normalize(Provider{ID: "c", Preset: "openrouter", Chat: "https://bedrock-mantle.us-east-1.api.aws/v1"}); p.Anthropic != "" {
		t.Errorf("filled in for another preset: %+v", p)
	}
}

// Mantle serves each OpenAI-style model at one of two prefixes: OpenAI's
// closed GPTs and xAI's Grok at /openai/v1, every other at /v1.
func TestMantleBase(t *testing.T) {
	const v1, oa = "https://bedrock-mantle.us-east-1.api.aws/v1", "https://bedrock-mantle.us-east-1.api.aws/openai/v1"
	for _, tc := range []struct {
		base, model string
		other       bool
		want        string
	}{
		{v1, "openai.gpt-oss-120b", false, v1},
		{v1, "google.gemma-3-27b-it", false, v1},
		{v1, "openai.gpt-5.5", false, oa},
		{v1, "openai.gpt-6-sol", false, oa},
		{v1, "xai.grok-4.3", false, oa},
		{oa, "openai.gpt-oss-120b", false, v1},
		{oa + "/", "xai.grok-4.3", false, oa},
		// learned to be at the other one
		{v1, "newmaker.model-1", true, oa},
		{v1, "openai.gpt-5.5", true, v1},
		// a URL the user typed some other way is theirs
		{"https://proxy.example.com/mantle", "openai.gpt-5.5", false, "https://proxy.example.com/mantle"},
	} {
		if got := MantleBase(tc.base, tc.model, tc.other); got != tc.want {
			t.Errorf("MantleBase(%q, %q, %v) = %q, want %q", tc.base, tc.model, tc.other, got, tc.want)
		}
	}
}
