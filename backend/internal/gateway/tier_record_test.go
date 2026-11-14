package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1784: the served tier is read only where a reply says it, in the shapes
// OpenAI's and Anthropic's APIs document, and never taken from the ask.
func TestUpstreamTierShapes(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`{"id":"c1","object":"chat.completion.chunk","service_tier":"default","choices":[]}`, "default"},
		{`{"type":"response.completed","response":{"id":"r1","service_tier":"priority","usage":{}}}`, "priority"},
		{`{"id":"r1","object":"response","service_tier":"flex"}`, "flex"},
		{`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":3,"service_tier":"priority"}}}`, "priority"},
		{`{"type":"message_delta","usage":{"output_tokens":3,"service_tier":"standard"}}`, "standard"},
		{`{"type":"response.completed","response":{"id":"r1","usage":{}}}`, ""},
		{`{"service_tier":null}`, ""},
		{`{"service_tier":"` + strings.Repeat("x", 40) + `"}`, ""},
		{`not json "service_tier"`, ""},
	} {
		if got := upstreamTier([]byte(c.body)); got != c.want {
			t.Errorf("%s: %q, want %q", c.body, got, c.want)
		}
	}

	s := &usageSniffer{}
	s.parse([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":3,"service_tier":"priority"}}}`))
	if s.u.Tier != "priority" {
		t.Errorf("sniffer: %+v", s.u)
	}

	for _, c := range []struct {
		proto      provider.Protocol
		body, want string
	}{
		{provider.Anthropic, `{"speed":"fast"}`, "fast"},
		{provider.Anthropic, `{"service_tier":"standard_only"}`, "standard_only"},
		{provider.Responses, `{"service_tier":"ultrafast"}`, "ultrafast"},
		{provider.Chat, `{"service_tier":1}`, ""},
		{provider.Gemini, `{"service_tier":"priority"}`, ""},
		// a long body, asked through its split (topValue): the tier after the
		// conversation, one inside it that isn't the request's, none at all
		{provider.Responses, `{"input":[{"role":"user","content":"` + strings.Repeat("words ", 8000) + `"}],"service_tier":"priority"}`, "priority"},
		{provider.Responses, `{"input":[{"role":"user","content":"x","service_tier":"flex","pad":"` + strings.Repeat("words ", 8000) + `"}]}`, ""},
		{provider.Anthropic, `{"messages":[{"role":"user","content":"` + strings.Repeat("words ", 8000) + `"}],"speed":"fast"}`, "fast"},
		{provider.Chat, `{"messages":[{"role":"user","content":"` + strings.Repeat("words ", 8000) + `"}],"service_tier":1}`, ""},
	} {
		if got := bodyTier(c.proto, []byte(c.body)); got != c.want {
			t.Errorf("%s %s: %q", c.proto, c.body, got)
		}
	}
}
