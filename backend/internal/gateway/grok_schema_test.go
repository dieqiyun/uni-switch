package gateway

import (
	"bytes"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

func TestECMAClassForRust(t *testing.T) {
	for _, tc := range [][2]string{
		// #2317's pattern: "[" inside a class is a character
		{`^[^\s[\]]+$`, `^[^\s\[\]]+$`},
		{`^[^\s\[\]]+$`, `^[^\s\[\]]+$`},
		{`[[:alpha:]]`, `[\[:alpha:]]`},
		{`[a&&b]`, `[a&\&b]`},
		{`[+--]`, `[+-\-]`},
		{`[~~]`, `[~\~]`},
		// outside a class nothing changes
		{`a&&b--c~~d\[x`, `a&&b--c~~d\[x`},
		{`^https?://[a-z]+\.[a-z]{2,}$`, `^https?://[a-z]+\.[a-z]{2,}$`},
		{`\\[a[b]`, `\\[a\[b]`},
	} {
		if got := ecmaClassForRust(tc[0]); got != tc[1] {
			t.Errorf("%q: got %q, want %q", tc[0], got, tc[1])
		}
	}
}

// grok2317Tools is the tool schema #2317's error names: a breakpoint's
// class_name and selector, each with ^[^\s[\]]+$ in an allOf, in one of a
// oneOf, as an Anthropic → Chat translation sends it.
const grok2317Tools = `"tools":[{"type":"function","function":{"name":"set_breakpoints","description":"<a> & b","parameters":{"type":"object","properties":{"breakpoints":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"line":{"type":"integer"}}},{"type":"object","properties":{"class_name":{"allOf":[{"type":"string"},{"pattern":"^[^\\s[\\]]+$"}]},"selector":{"allOf":[{"type":"string"},{"pattern":"^[^\\s[\\]]+$"}]}}}]}}}}}}]`

func TestGrokToolPatterns2317(t *testing.T) {
	body := []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"[^\\s[\\]]"}],` + grok2317Tools + `}`)
	relay := provider.Provider{ID: "gwork", Chat: "https://relay.example/v1"}
	out := grokToolPatterns(relay, provider.Chat, body)
	at := "tools.0.function.parameters.properties.breakpoints.items.oneOf.1.properties."
	for _, f := range []string{"class_name", "selector"} {
		if got := gjson.GetBytes(out, at+f+".allOf.1.pattern").String(); got != `^[^\s\[\]]+$` {
			t.Errorf("%s pattern = %q", f, got)
		}
	}
	// only the patterns change: the message and the rest stay byte for byte
	want := bytes.ReplaceAll(body, []byte(`"^[^\\s[\\]]+$"`), []byte(`"^[^\\s\\[\\]]+$"`))
	if !bytes.Equal(out, want) {
		t.Fatalf("body changed beyond its patterns:\n%s", out)
	}
	// a relay's or router's name for Grok, and xAI's own API by any name
	for _, model := range []string{"xai/grok-4.7", "x-ai/grok-4"} {
		b := bytes.Replace(body, []byte(`"grok-4.6"`), []byte(`"`+model+`"`), 1)
		if bytes.Equal(grokToolPatterns(relay, provider.Chat, b), b) {
			t.Errorf("%s: pattern not fixed", model)
		}
	}
	xai := provider.Provider{ID: "xai", Chat: "https://api.x.ai/v1"}
	b := bytes.Replace(body, []byte(`"grok-4.6"`), []byte(`"my-alias"`), 1)
	if bytes.Equal(grokToolPatterns(xai, provider.Chat, b), b) {
		t.Error("api.x.ai: pattern not fixed")
	}
}

func TestGrokToolPatternsOtherModelsAsTheyCame(t *testing.T) {
	relay := provider.Provider{ID: "gwork", Chat: "https://relay.example/v1"}
	for _, model := range []string{"gpt-6-astra", "claude-sonnet-5", "deepseek-v4", "kimi-k3"} {
		body := []byte(`{"model":"` + model + `",` + grok2317Tools + `}`)
		if out := grokToolPatterns(relay, provider.Chat, body); !bytes.Equal(out, body) {
			t.Errorf("%s: body changed:\n%s", model, out)
		}
	}
	// a Grok request whose patterns need nothing passes through as it came
	body := []byte(`{"model":"grok-4.6","tools":[{"name":"x","input_schema":{"properties":{"u":{"pattern":"^https?://[a-z]+$"}}}}]}`)
	if out := grokToolPatterns(relay, provider.Anthropic, body); !bytes.Equal(out, body) {
		t.Errorf("unchanged body re-encoded:\n%s", out)
	}
}
