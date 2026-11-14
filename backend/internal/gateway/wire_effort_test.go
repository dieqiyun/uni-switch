package gateway

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

var anthropicHi = sse(
	`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m1","usage":{"input_tokens":7}}}`,
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
	`data: {"type":"content_block_stop","index":0}`,
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
	`data: {"type":"message_stop"}`)

var chatHi = sse(
	`data: {"id":"c1","model":"m1","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
	`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
	`data: [DONE]`)

// responsesHi is the same reply from a Responses API, which reads its own
// events only: a Chat stream from it is no reply (#1933).
var responsesHi = sse(
	`data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"hi"}`,
	`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":1}}}`)

// A user-added provider whose model's levels no one lists (#1636,
// jswysnemc): a client asking reasoning off or minimal on another API
// than the provider's went at another level, while the request log and
// Routing said the one asked. Reasoning off went to an Anthropic API as
// thinking at a 4096-token budget; it goes as thinking off now. Where the
// level asked can't go as it is — none and minimal read as low for a
// model not known to stop (effortOf) — the route keeps what was asked
// and the try and the log what went.
func TestTranslatedEffortLoggedAsSent(t *testing.T) {
	for _, c := range []struct {
		up                 provider.Protocol
		path, body         string
		asked, went, thing string // thing: what the upstream body asks, as "field=value"
	}{
		{provider.Anthropic, "/v1/responses", `{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"none"}}`, "none", "none", "thinking=disabled"},
		{provider.Anthropic, "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`, "none", "none", "thinking=disabled"},
		{provider.Anthropic, "/v1/responses", `{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"high"}}`, "high", "high", "thinking=enabled"},
		{provider.Chat, "/v1/responses", `{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"minimal"}}`, "minimal", "low", "reasoning_effort=low"},
		{provider.Chat, "/v1/responses", `{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"none"}}`, "none", "low", "reasoning_effort=low"},
		{provider.Chat, "/v1/responses", `{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"xhigh"}}`, "xhigh", "xhigh", "reasoning_effort=xhigh"},
		{provider.Responses, "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`, "none", "low", "reasoning.effort=low"},
		// the same API: it goes as it came, and the log says so
		{provider.Chat, "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`, "none", "none", "reasoning_effort=none"},
	} {
		f := &fake{t: t, reply: chatHi}
		switch c.up {
		case provider.Anthropic:
			f.reply = anthropicHi
		case provider.Responses:
			f.reply = responsesHi
		}
		setup(t, c.up, f)
		s := New()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", c.path, strings.NewReader(c.body)))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", c.body, rec.Code, rec.Body)
		}
		var got map[string]any
		json.Unmarshal(f.got, &got)
		field, want, _ := strings.Cut(c.thing, "=")
		var v any
		switch field {
		case "thinking":
			m, _ := got["thinking"].(map[string]any)
			v = m["type"]
		case "reasoning.effort":
			m, _ := got["reasoning"].(map[string]any)
			v = m["effort"]
		default:
			v = got[field]
		}
		if v != want {
			t.Errorf("%s to %s: sent %s", c.body, c.up, f.got)
		}
		r := lastRoute(s)
		if r.Effort != c.asked || len(r.Tries) != 1 || r.Tries[0].Effort != c.went {
			t.Errorf("%s to %s: route at %q, tries %+v; want asked %q, went %q", c.body, c.up, r.Effort, r.Tries, c.asked, c.went)
		}
		recs := usage.Load(time.Time{})
		if len(recs) == 0 || recs[len(recs)-1].Effort != c.went {
			t.Errorf("%s to %s: logged %+v, want %q", c.body, c.up, recs, c.went)
		}
	}
}

// A vendor's model on its Anthropic API that won't stop thinking (Z.ai's
// GLM-5.3, #699) refuses the thinking off a Codex asking none is now sent
// (#1636): asked again thinking at its least, as before, and from then on
// asked so at once.
func TestTranslatedThinkingOffRefused(t *testing.T) {
	for i, refusal := range []string{
		`GLM-5.3 always engages in thinking and cannot be disabled`,
		// MiniMax's and Volcengine Ark's wordings (#1141)
		`invalid params, model \"MiniMax-M3.1-Flash-Preview\" requires adaptive thinking; thinking.type=\"disabled\" (including reasoning.effort=none) is not allowed (2013)`,
		"thinking.type `disabled` is not supported by this model Request id: 0217",
		// Kimi Code's K3.1 (ZCode sends no thinking at all, #3739)
		`Kimi Code (China): K3.1 Pro does not support disabled thinking mode. Please enable thinking and try again, or choose another model.`,
		// Sub2API's gpt-6.1-sol reads disabled as reasoning effort none (#1465)
		`gpt-6.1-sol does not support reasoning effort \"none\"; use low, medium, high, xhigh or max`,
		// #699's Chinese, escaped as \uXXXX the way a relay's
		// json.dumps sends it (#1669)
		`\u8be5\u6a21\u578b\u59cb\u7ec8\u652f\u6301\u601d\u8003\uff0c\u4e0d\u53ef\u5173\u95ed`,
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) { translatedThinkingOffRefused(t, refusal) })
	}
}

func translatedThinkingOffRefused(t *testing.T, refusal string) {
	var sent []string
	f := &fake{t: t, reply: anthropicHi}
	f.refuse = func(b []byte) (int, string) {
		var v struct {
			Thinking struct {
				Type string `json:"type"`
			} `json:"thinking"`
		}
		json.Unmarshal(b, &v)
		sent = append(sent, v.Thinking.Type)
		if v.Thinking.Type == "disabled" {
			return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"` + refusal + `"}}`
		}
		return 0, ""
	}
	setup(t, provider.Anthropic, f)
	s := New()
	for turn, want := range []string{"disabled,enabled", "enabled"} {
		sent = nil
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"none"}}`)))
		if rec.Code != 200 {
			t.Fatalf("turn %d: %d %s", turn+1, rec.Code, rec.Body)
		}
		if strings.Join(sent, ",") != want {
			t.Errorf("turn %d: thinking sent %v, want %s", turn+1, sent, want)
		}
		if r := lastRoute(s); len(r.Tries) != 1 || r.Tries[0].Effort != "low" {
			t.Errorf("turn %d: tries %+v, want it logged at low", turn+1, r.Tries)
		}
	}
}

// Kimi Code's K3.1 Pro refuses reasoning off on its Chat endpoint too, in
// the words its Messages endpoint uses (#3739), with no levels named. A
// Chat client's reasoning_effort none, relayed, and a Responses client's
// effort none, translated to Chat, are asked again at the model's lowest,
// and at it from the start after that; another 400 goes back as it came.
func TestKimiChatThinkingOffRefused(t *testing.T) {
	const refusal = `{"error":{"message":"Kimi Code (China): K3.1 Pro does not support disabled thinking mode. Please enable thinking and try again, or choose another model.","type":"invalid_request_error"}}`
	for _, c := range []struct{ name, path, body string }{
		{"relayed", "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`},
		{"translated", "/v1/responses", `{"model":"m1","stream":true,"input":"hi","reasoning":{"effort":"none"}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			var sent []string
			f := &fake{t: t, reply: chatHi}
			f.refuse = func(b []byte) (int, string) {
				var v struct {
					Effort string `json:"reasoning_effort"`
				}
				json.Unmarshal(b, &v)
				sent = append(sent, v.Effort)
				if v.Effort == "none" {
					return 400, refusal
				}
				return 0, ""
			}
			setup(t, provider.Chat, f)
			// Kimi Code's catalog gives its toggled models none
			// (reasoningOffProviders), as models.dev lists K3.1 Pro
			if err := provider.SetModelEfforts("fake/m1", []string{"none", "low", "high"}); err != nil {
				t.Fatal(err)
			}
			s := New()
			for turn, want := range []string{"none,low", "low"} {
				sent = nil
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", c.path, strings.NewReader(c.body)))
				if rec.Code != 200 {
					t.Fatalf("turn %d: %d %s", turn+1, rec.Code, rec.Body)
				}
				if strings.Join(sent, ",") != want {
					t.Errorf("turn %d: reasoning_effort sent %v, want %s", turn+1, sent, want)
				}
			}
			f.refuse = func([]byte) (int, string) {
				return 400, `{"error":{"message":"Kimi Code (China): max_tokens is too large","type":"invalid_request_error"}}`
			}
			f.calls = 0
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", c.path, strings.NewReader(c.body)))
			if rec.Code != 400 || f.calls != 1 {
				t.Errorf("another 400: %d after %d calls", rec.Code, f.calls)
			}
		})
	}
}
