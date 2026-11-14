package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// WorkBuddy AI's last chunk for gemini-3.5-flash, as it came on 10-16:
// usage.credit is what the request cost in the plan's credits, the figure
// WorkBuddy's app shows on a reply as "Credits Consumed" (#2512)
const wbCreditUsage = `"usage":{"prompt_tokens":9,"completion_tokens":12,"total_tokens":21,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":12,"rejected_prediction_tokens":0,"cached_tokens":0},"prompt_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0,"cached_tokens":0},"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":9,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"prompt_cache_write_tokens":0,"completion_thinking_tokens":12,"credit":0.01,"cached_tokens":0}`

// and a free model's: credit 0 is said, so it is known to have cost none
const wbFreeUsage = `"usage":{"prompt_tokens":166,"completion_tokens":10,"total_tokens":176,"prompt_tokens_details":{"cached_tokens":128},"completion_thinking_tokens":0,"credit":0,"cached_tokens":0}`

// The ledger keeps what each request said it cost in credits, however the
// agent asked: a chat stream passed through, a whole reply, or translated
// for Claude Code. A reply that says no credit leaves it unknown, not 0.
func TestPlanCreditsReachTheLedger(t *testing.T) {
	stream := func(usage string) string {
		return sse(`data: {"id":"c","model":"m1","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
			`data: {"id":"c","model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+usage+`}`, "data: [DONE]")
	}
	whole := func(usage string) string {
		return `{"id":"c","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` + usage + `}`
	}
	const chat = `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`
	const chatStream = `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	const messages = `{"model":"m1","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	none := `"usage":{"prompt_tokens":9,"completion_tokens":12}`
	for _, c := range []struct {
		name, ctype, reply, path, request string
		want                              *float64
	}{
		{"chat stream", "", stream(wbCreditUsage), "/v1/chat/completions", chatStream, ptr(0.01)},
		{"whole reply", "application/json", whole(wbCreditUsage), "/v1/chat/completions", chat, ptr(0.01)},
		{"translated for Claude Code", "", stream(wbCreditUsage), "/v1/messages", messages, ptr(0.01)},
		{"a free model", "", stream(wbFreeUsage), "/v1/chat/completions", chatStream, ptr(0.0)},
		{"translated, free", "", stream(wbFreeUsage), "/v1/messages", messages, ptr(0.0)},
		{"no credit said", "", stream(none), "/v1/chat/completions", chatStream, nil},
		{"translated, no credit said", "", stream(none), "/v1/messages", messages, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			setup(t, provider.Chat, &fake{ctype: c.ctype, reply: c.reply})
			if code, body := post(t, c.path, c.request); code != 200 {
				t.Fatalf("gateway: %d %s", code, body)
			}
			r := lastUsage(t)
			switch {
			case c.want == nil && r.Credits != nil:
				t.Fatalf("credits %v from a reply that said none", *r.Credits)
			case c.want != nil && (r.Credits == nil || *r.Credits != *c.want):
				t.Fatalf("credits %v, want %v", r.Credits, *c.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
