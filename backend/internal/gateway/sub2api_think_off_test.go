package gateway

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// sub2apiNoneRefused is Sub2API's answer, on its Messages API, to a
// request for gpt-6.1-sol with thinking turned off, which it reads as
// reasoning effort none (anthropicReasoningEffort) and turns away (#1465,
// MengXiangxi's message, from ValidateGPT61SolReasoningEffort).
const sub2apiNoneRefused = `{"type":"error","error":{"type":"invalid_request_error","message":"gpt-6.1-sol does not support reasoning effort \"none\"; use low, medium, high, xhigh or max"}}`

// #1465: Claude Code's auto mode classifier, relayed as it was sent to a
// Sub2API's gpt-6.1-sol, turns thinking off; Sub2API refused every one,
// and Claude Code blocked every action. Asked again with thinking left to
// the model, and so from the start from then on, its title asks (which
// uni-switch turns thinking off for) included. Another 400 naming the levels,
// for another level, is the agent's to see.
func TestSub2APIThinkingOffRefused(t *testing.T) {
	var sent []string
	f := &fake{t: t, reply: anthropicHi}
	f.refuse = func(b []byte) (int, string) {
		th := gjson.GetBytes(b, "thinking.type").String()
		sent = append(sent, th)
		if th == "disabled" {
			return 400, sub2apiNoneRefused
		}
		return 0, ""
	}
	setup(t, provider.Anthropic, f)
	srv := New()
	for turn, want := range [][]string{{"disabled", ""}, {""}} {
		sent = nil
		code, body := postTo(t, srv, "/v1/messages", autoModeAsk("m1"))
		if code != 200 || len(sent) != len(want) || sent[len(sent)-1] != "" {
			t.Fatalf("turn %d: %d %s, thinking sent %q, want %q", turn+1, code, body, sent, want)
		}
		if !bytes.Contains(f.got, []byte("tidy up the build")) {
			t.Errorf("turn %d: sent %s", turn+1, f.got)
		}
	}
	sent = nil
	if code, body := postTo(t, srv, "/v1/messages", `{"model":"m1","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"title?"}]}`); code != 200 || len(sent) != 1 || sent[0] != "" {
		t.Errorf("title: %d %s, thinking sent %q", code, body, sent)
	}

	f.calls = 0
	f.refuse = func([]byte) (int, string) {
		return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"reasoning effort \"xhigh\" exceeds this group's limit; use low, medium or high"}}`
	}
	if code, _ := post(t, "/v1/messages", autoModeAsk("m1")); code != 400 || f.calls != 1 {
		t.Errorf("another level refused: %d after %d calls", code, f.calls)
	}
}

func TestRefusesThinkingOff(t *testing.T) {
	for b, want := range map[string]bool{
		sub2apiNoneRefused: true,
		`{"error":{"message":"Sub2API: gpt-6.1-sol does not support reasoning effort \"none\"; use low, medium, high, xhigh or max","type":"invalid_request_error"},"type":"error"}`: true,
		`GLM-5.3 always engages in thinking and cannot be disabled`:                                              true,
		`{"error":{"message":"reasoning effort \"xhigh\" exceeds this group's limit; use low, medium or high"}}`: false,
		`{"error":{"message":"None of the tools match; use low, medium or high"}}`:                               false,
	} {
		if got := refusesThinkingOff([]byte(b)); got != want {
			t.Errorf("%s: %v, want %v", b, got, want)
		}
	}
}
