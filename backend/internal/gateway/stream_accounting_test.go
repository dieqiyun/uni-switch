package gateway

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// Once text has been sent, an upstream error leaves the HTTP status at
// 200. It is still a failed request wherever the gateway records it.
func TestStreamFailureAccounting(t *testing.T) {
	for _, client := range []struct {
		name, path, body, failed, completed string
	}{
		{"native", "/v1/responses", `{"model":"fake/m1","stream":true,"input":"hi"}`, "event: response.failed", "event: response.completed"},
		{"translated", "/v1/messages", `{"model":"fake/m1","stream":true,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`, "event: error", "event: message_stop"},
	} {
		for _, outcome := range []string{"completed", "failed"} {
			t.Run(client.name+"/"+outcome, func(t *testing.T) {
				fresh(t)
				reply := sse(
					`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r1","model":"m1","status":"in_progress"}}`,
					`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"partial text"}`,
				)
				const failure = "synthetic stream failure"
				if outcome == "failed" {
					reply += sse(`event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"id":"r1","status":"failed","error":{"type":"server_error","message":"` + failure + `"}}}`)
				} else {
					reply += sse(`event: response.completed` + "\n" + `data: {"type":"response.completed","response":{"id":"r1","model":"m1","status":"completed","usage":{"input_tokens":3,"output_tokens":1}}}`)
				}
				setup(t, provider.Responses, &fake{reply: reply})
				s := New()
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("POST", client.path, strings.NewReader(client.body)))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "partial text") {
					t.Fatalf("stream: %d %s", w.Code, w.Body)
				}
				failed := outcome == "failed"
				if strings.Contains(w.Body.String(), client.failed) != failed || strings.Contains(w.Body.String(), client.completed) == failed {
					t.Fatalf("wrong stream ending: %s", w.Body)
				}
				recent := s.Recent()
				state := s.Trace(context.Background(), 0, 0)
				records := usage.Load(time.Time{})
				if len(recent) != 1 || len(state.Routes) != 1 || len(records) != 1 {
					t.Fatalf("recent=%d routes=%d usage=%d, want one each", len(recent), len(state.Routes), len(records))
				}
				route, record := state.Routes[0], records[0]
				if !route.Done || route.Status != 200 || record.Status != 200 || record.RouteID != route.ID {
					t.Fatalf("route: %+v; usage: %+v", route, record)
				}
				wantError, wantErrors := "", 0
				if failed {
					wantError, wantErrors = failure, 1
				}
				if recent[0].Error != wantError || route.Error != wantError || record.Error != wantError || record.Failed() != failed {
					t.Errorf("errors: recent=%q route=%q usage=%q failed=%t, want %q", recent[0].Error, route.Error, record.Error, record.Failed(), wantError)
				}
				if state.Totals.Requests != 1 || state.Totals.Errors != wantErrors {
					t.Errorf("totals: %+v, want one request and %d errors", state.Totals, wantErrors)
				}
			})
		}
	}
}

// Native stream errors are relayed byte for byte, including an
// error event without a final newline, and still reach the request log.
// Only the model a reply names is the one the agent asked for.
func TestNativeStreamFailureAccounting(t *testing.T) {
	const failure = "synthetic stream failure"
	for _, c := range []struct {
		name, wantError, wantType string
		proto                     provider.Protocol
		path, body, reply         string
	}{
		{"chat", failure, "server_error", provider.Chat, "/v1/chat/completions", `{"model":"fake/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			sse(`data: {"choices":[{"delta":{"content":"partial text"}}]}`, `data: {"error":{"type":"server_error","message":"`+failure+`"}}`)},
		{"anthropic", failure, "server_error", provider.Anthropic, "/v1/messages", `{"model":"fake/m1","stream":true,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`,
			anthropicStart + anthropicText + sse(`event: error`+"\n"+`data: {"type":"error","error":{"type":"server_error","message":"`+failure+`"}}`)},
		{"responses error", failure, "server_error", provider.Responses, "/v1/responses", `{"model":"fake/m1","stream":true,"input":"hi"}`,
			sse(`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"partial text"}`, `event: error`+"\n"+`data: {"type":"error","code":"server_error","message":"`+failure+`"}`)},
		{"responses failed without message", "upstream stream failed", "", provider.Responses, "/v1/responses", `{"model":"fake/m1","stream":true,"input":"hi"}`,
			sse(`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"partial text"}`, `event: response.failed`+"\n"+`data: {"type":"response.failed","response":{"id":"r1","status":"failed"}}`)},
		{"error event without data", "upstream stream failed", "", provider.Anthropic, "/v1/messages", `{"model":"fake/m1","stream":true,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`,
			anthropicStart + anthropicText + sse(`event: error`)},
	} {
		for _, ending := range []string{"newline", "unterminated"} {
			t.Run(c.name+"/"+ending, func(t *testing.T) {
				fresh(t)
				reply := c.reply
				if ending == "unterminated" {
					reply = strings.TrimRight(reply, "\n")
				}
				setup(t, c.proto, &fake{reply: reply})
				s := New()
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("POST", c.path, strings.NewReader(c.body)))
				if w.Code != 200 || w.Body.String() != namedAs(reply, "fake/m1") {
					t.Fatalf("native stream was changed: %d %s", w.Code, w.Body)
				}
				state, recent := s.Trace(context.Background(), 0, 0), s.Recent()
				if len(state.Routes) != 1 || len(recent) != 1 {
					t.Fatalf("routes=%d recent=%d, want one each", len(state.Routes), len(recent))
				}
				record := lastRecord(t)
				if recent[0].Error != c.wantError || state.Routes[0].Error != c.wantError || record.Error != c.wantError || record.ErrType != c.wantType || record.Status != 200 {
					t.Errorf("recent error=%q route error=%q; usage: %+v", recent[0].Error, state.Routes[0].Error, record)
				}
				if state.Totals.Errors != 1 {
					t.Errorf("routing errors=%d, want 1", state.Totals.Errors)
				}
			})
		}
	}
}
