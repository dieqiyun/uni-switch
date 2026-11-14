package gateway

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What Claude Code 2.1.316 wrote, in -p and in its interactive mode
// (claudetui), against an upstream whose claude-opus-5-5 stops each
// answer with stop_reason refusal and whose other models answer: it asks
// again once, then on its fallback model (claude-opus-4-8 for an API
// key; Opus 5 for the reporter's subscription), which answers. Session
// ids, uuids and most usage fields are left out; the rest is as written.
var (
	refusedAttempt = []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_fake1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}},"ttft_ms":16,"thinking_display":"updates","api_message_id":"msg_fake1"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"api_message_id":"msg_fake1"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial "}},"api_message_id":"msg_fake1"}`,
		`{"type":"assistant","message":{"id":"msg_fake1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Partial "}],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1},"context_management":null},"request_id":"req_1"}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"api_message_id":"msg_fake1"}`,
		`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"}},"usage":{"output_tokens":3}},"api_message_id":"msg_fake1"}`,
		`{"type":"stream_event","event":{"type":"message_stop"},"api_message_id":"msg_fake1"}`,
		`{"type":"system","subtype":"informational","content":"Opus 5.5's safeguards stopped the response above · continuing once with that noted","level":"notice"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Your response above was stopped by a safety classifier — this is not a tool or API error."}]},"isSynthetic":true,"refused_message_id":"msg_fake1"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_fake2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}},"ttft_ms":4,"thinking_display":"updates","api_message_id":"msg_fake2"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"api_message_id":"msg_fake2"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial "}},"api_message_id":"msg_fake2"}`,
		`{"type":"assistant","message":{"id":"msg_fake2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Partial "}],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1},"context_management":null},"request_id":"req_2"}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"api_message_id":"msg_fake2"}`,
		`{"type":"system","subtype":"model_refusal_fallback","trigger":"refusal","direction":"retry","scope":"session","original_model":"claude-opus-5-5","fallback_model":"claude-opus-4-8","request_id":"req_2","api_refusal_category":"cyber","api_refusal_explanation":"flagged","retracted_message_uuids":["1e3473df"],"content":"Opus 5.5's safeguards flagged this session."}`,
		`{"type":"stream_event","event":{"type":"message_delta","context_management":null,"delta":{"container":null,"stop_details":null,"stop_reason":"refusal","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":3}},"api_message_id":"msg_fake2"}`,
		`{"type":"stream_event","event":{"type":"message_stop"},"api_message_id":"msg_fake2"}`,
	}
	fallbackAnswer = []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_fake3","type":"message","role":"assistant","model":"claude-opus-4-8","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}},"ttft_ms":3,"thinking_display":"updates","api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"FALLBACK ANSWER from claude-opus-4-8"}},"api_message_id":"msg_fake3"}`,
		`{"type":"assistant","message":{"id":"msg_fake3","type":"message","role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"FALLBACK ANSWER from claude-opus-4-8"}],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1},"context_management":null},"request_id":"req_3","supersedes":["1e3473df"]}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null,"stop_details":null},"usage":{"output_tokens":3}},"api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"message_stop"},"api_message_id":"msg_fake3"}`,
		`{"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","result":"FALLBACK ANSWER from claude-opus-4-8"}`,
	}
	// the fallback model refuses too: Claude Code has no model left
	fallbackRefuses = []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_fake3","type":"message","role":"assistant","model":"claude-opus-4-8","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}},"ttft_ms":3,"thinking_display":"updates","api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial "}},"api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"api_message_id":"msg_fake3"}`,
		`{"type":"system","subtype":"model_refusal_no_fallback","original_model":"claude-opus-4-8","request_id":"req_3","api_refusal_category":"cyber","api_refusal_explanation":"flagged","content":""}`,
		`{"type":"assistant","message":{"id":"d6d30e8b-8c8c-407d-97c6-c97694bd508d","model":"<synthetic>","role":"assistant","stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"},"stop_reason":"refusal","stop_sequence":"","type":"message","usage":{"input_tokens":0,"output_tokens":0},"content":[{"type":"text","text":"API Error: Opus 4.8's safeguards flagged this message."}]},"error":"invalid_request","request_id":"req_3","is_api_error_message":true,"refused_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"}},"usage":{"output_tokens":3}},"api_message_id":"msg_fake3"}`,
		`{"type":"stream_event","event":{"type":"message_stop"},"api_message_id":"msg_fake3"}`,
		`{"type":"result","subtype":"success","is_error":true,"stop_reason":"refusal","result":"API Error: Opus 4.8's safeguards flagged this message."}`,
	}
	// the interactive mode's lines: whole messages from the transcript,
	// where Claude Code leaves out the attempt it retracted (msg_fake2)
	tuiRefused = `{"message":{"id":"msg_fake1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Partial "}],"stop_reason":"refusal","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":3},"stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"}},"request_id":"req_1","type":"assistant"}`
	tuiAnswer  = []string{
		`{"message":{"id":"msg_fake3","type":"message","role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"FALLBACK ANSWER from claude-opus-4-8"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":3},"stop_details":null},"request_id":"req_3","type":"assistant"}`,
		`{"is_error":false,"subtype":"success","type":"result"}`,
	}
	tuiNoFallback = []string{
		`{"message":{"id":"msg_fake3","type":"message","role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"Partial "}],"stop_reason":"refusal","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":3},"stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"}},"request_id":"req_3","type":"assistant"}`,
		`{"error":"invalid_request","message":{"id":"90717bc6-73fe-4977-a7d4-6f378251be08","model":"<synthetic>","role":"assistant","stop_details":{"type":"refusal","category":"cyber","explanation":"flagged"},"stop_reason":"refusal","stop_sequence":"","type":"message","usage":{"input_tokens":0,"output_tokens":0},"content":[{"type":"text","text":"API Error: Opus 4.8's safeguards flagged this message."}]},"type":"assistant"}`,
		`{"api_error_status":0,"is_error":true,"result":"API Error: Opus 4.8's safeguards flagged this message.","subtype":"error_during_execution","type":"result"}`,
	}
)

func joined(parts ...[]string) []string {
	var all []string
	for _, p := range parts {
		all = append(all, p...)
	}
	return all
}

// claudeEvents runs lines through readOutput and says what the reply's
// segment was told, up to its end.
func claudeEvents(t *testing.T, lines []string) []Event {
	t.Helper()
	run := &subscriptionRun{}
	segment := run.attach()
	pr, pw := io.Pipe()
	go run.readOutput(pr)
	go func() {
		for _, l := range lines {
			pw.Write([]byte(l + "\n"))
		}
	}()
	t.Cleanup(func() { pw.Close() })
	var evs []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-segment:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-timeout:
			t.Fatalf("the reply never ended: %+v", evs)
		}
	}
}

// Claude Code falls back from a refusal to another model (Chixi_Fang on
// X): its fallback model's answer is the reply, with an end_turn and no
// refusal in it, so the gateway neither takes the request to another
// account nor tells the agent it was refused. The old code ended the reply
// at the first attempt's refusal: "Partial " and a refusal, the answer
// lost.
func TestClaudeRefusalFallbackAnswers(t *testing.T) {
	was := unstreamedQuiet
	unstreamedQuiet = 50 * time.Millisecond
	t.Cleanup(func() { unstreamedQuiet = was })
	for name, c := range map[string]struct {
		lines []string
		text  string
	}{
		// what was streamed can't be taken back: the retracted attempts'
		// words stay before the answer
		"print": {joined(refusedAttempt, fallbackAnswer), "Partial Partial FALLBACK ANSWER from claude-opus-4-8"},
		"tui":   {append([]string{tuiRefused}, tuiAnswer...), "Partial FALLBACK ANSWER from claude-opus-4-8"},
	} {
		t.Run(name, func(t *testing.T) {
			text, stop, _, _, raw, ended, ok := claudeReply(t, &subscriptionRun{}, c.lines, true)
			if text != c.text || stop != "end_turn" || ended != "stop" || !ok {
				t.Fatalf("reply %q stop %q, run ended %q %v:\n%s", text, stop, ended, ok, raw)
			}
			// what the gateway's refusal handling reads (streamEvent)
			if strings.Contains(raw, "refusal") || strings.Contains(raw, "cyber") {
				t.Fatalf("the reply carries the retracted refusal:\n%s", raw)
			}
			if n := strings.Count(raw, "event: message_start"); n != 1 {
				t.Fatalf("%d message starts:\n%s", n, raw)
			}
			// the model that answered is the one recorded as serving it
			served := ""
			for _, ev := range claudeEvents(t, c.lines) {
				if ev.Usage.Served != "" {
					served = ev.Usage.Served
				}
			}
			if served != "claude-opus-4-8" {
				t.Fatalf("served %q", served)
			}
		})
	}
}

// With no model left to fall back on, Claude Code ends the turn on the
// refusal, and the reply is a refusal still, its classifier's details
// with it, for the gateway's refusal handling (refusal_accounts_test.go).
func TestClaudeRefusalWithNoFallback(t *testing.T) {
	was := unstreamedQuiet
	unstreamedQuiet = 50 * time.Millisecond
	t.Cleanup(func() { unstreamedQuiet = was })
	for name, lines := range map[string][]string{
		"print": joined(refusedAttempt, fallbackRefuses),
		"tui":   append([]string{tuiRefused}, tuiNoFallback...),
	} {
		t.Run(name, func(t *testing.T) {
			_, stop, _, _, raw, ended, _ := claudeReply(t, &subscriptionRun{}, lines, true)
			if stop != "refusal" || ended != "filter" || !strings.Contains(raw, `"category":"cyber"`) {
				t.Fatalf("stop %q, run ended %q:\n%s", stop, ended, raw)
			}
			if strings.Contains(raw, "API Error") {
				t.Fatalf("Claude Code's own error told as the reply:\n%s", raw)
			}
		})
	}
}

// A retry after the refusal that fails with a status of its own is that
// failure, as before.
func TestClaudeRefusalRetryFails(t *testing.T) {
	lines := append(append([]string{}, refusedAttempt[:7]...),
		`{"type":"result","subtype":"error_during_execution","is_error":true,"api_error_status":429,"result":"API Error: 429 rate limited"}`)
	_, stop, _, _, raw, _, ok := claudeReply(t, &subscriptionRun{}, lines, true)
	if stop == "end_turn" || ok || !strings.Contains(raw, "429 rate limited") {
		t.Fatalf("stop %q ok %v:\n%s", stop, ok, raw)
	}
}

// Through the gateway, with a spare account: the fallback's answer goes to
// the agent from the account it ran on, the spare isn't asked, and the
// try says which model answered.
func TestClaudeSubscriptionRefusalFallbackStays(t *testing.T) {
	said := filepath.Join(t.TempDir(), "lines")
	os.WriteFile(said, []byte(strings.Join(joined(refusedAttempt, fallbackAnswer), "\n")+"\n"), 0o600)
	log := claudeSubscriptionFirstSays(t, "    cat '"+said+"'\n")
	s := New()
	for _, ask := range []struct{ path, body, served string }{
		// a stream named its model as it began; the try records the
		// model that finished it, as a reply told whole names it
		{"/v1/messages", `{"model":"claude/claude-sonnet-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"ping"}]}`, "claude-opus-4-8"},
		{"/v1/messages", `{"model":"claude/claude-sonnet-5","max_tokens":50,"messages":[{"role":"user","content":"ping again"}]}`, "claude-opus-4-8"},
		{"/v1/responses", `{"model":"claude/claude-sonnet-5","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`, "claude-opus-4-8"},
	} {
		os.Remove(log)
		code, body := sendTo(s, ask.path, ask.body)
		b, _ := os.ReadFile(log)
		if code != 200 || !strings.Contains(body, "FALLBACK ANSWER") || strings.Contains(body, "pong") || strings.Join(strings.Fields(string(b)), ",") != "own" {
			t.Fatalf("%s: status %d: %s (runs %q)", ask.path, code, body, b)
		}
		r := lastRoute(s)
		if len(r.Tries) != 1 || r.Tries[0].Fail != "" || r.Tries[0].Served != ask.served {
			t.Fatalf("%s: tries: %+v", ask.path, r.Tries)
		}
	}
}
