package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What the ChatGPT backend sent Aliceonly's Codex on plus-B (#1418): a 200
// and the frames before a reply, nothing said, and 36s on the response
// failed with server_is_overloaded.
var overloadedLead = []string{
	`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"r0","status":"in_progress"}}`,
	`event: response.in_progress` + "\n" + `data: {"type":"response.in_progress","response":{"id":"r0","status":"in_progress"}}`,
	`event: codex.rate_limits` + "\n" + `data: {"type":"codex.rate_limits","plan_type":"plus","rate_limits":{"allowed":true}}`,
}

const overloadedFailed = `event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`

// A stream with nothing but its frames, held past 15s while its agent is
// kept alive with comments, stays held: its server_is_overloaded after
// that is a failure the group's next member answers, not the agent's
// error (#1418). Before, the frame that came after 15s let the stream
// through, and the failure 21s later reached Codex as the turn's end with
// sub2api, next in the group, never asked. A Gemini stream, whose agent
// takes no comment, is let through at 15s as before.
func TestLeadOnlyStreamHeldWhileKeptAlive(t *testing.T) {
	held := func(proto provider.Protocol) (*holdWriter, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		h := newHoldWriter(rec, true)
		h.alive, h.streams = &keptAlive{proto: proto}, true
		h.Header().Set("Content-Type", "text/event-stream")
		h.WriteHeader(200)
		io.WriteString(h, sse(overloadedLead[:2]...))
		h.since = time.Now().Add(-(holdLongest + 5*time.Second))
		io.WriteString(h, sse(overloadedLead[2]))
		io.WriteString(h, ": ping\n\n")
		return h, rec
	}
	h, rec := held(provider.Responses)
	if h.passing || strings.Contains(rec.Body.String(), "response.created") {
		t.Fatalf("let through after %s with nothing said: %q", holdLongest, rec.Body.String())
	}
	io.WriteString(h, sse(overloadedFailed))
	if !h.failed() || h.code() != 529 || !strings.Contains(h.failMsg, "server_is_overloaded") || strings.Contains(rec.Body.String(), "server_is_overloaded") {
		t.Fatalf("failed %v (%d %q); sent %q", h.failed(), h.code(), h.failMsg, rec.Body.String())
	}

	// held so long, it is let through as before
	h, rec = held(provider.Responses)
	h.since = time.Now().Add(-(holdLead + time.Second))
	io.WriteString(h, ": ping\n\n")
	if !h.passing || !strings.Contains(rec.Body.String(), "response.created") {
		t.Fatalf("held past %s: %q", holdLead, rec.Body.String())
	}

	h, rec = held(provider.Gemini)
	if !h.passing || !strings.Contains(rec.Body.String(), "response.created") {
		t.Fatalf("a stream whose agent takes no comment held past %s", holdLongest)
	}
}

// slowFrames is a Responses upstream that sends the frames before a reply
// at once and, after wait, its end: then (the request's n-th, from 0).
type slowFrames struct {
	wait time.Duration
	then func(n int) string
	mu   sync.Mutex
	n    int
}

func (s *slowFrames) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	if !strings.HasSuffix(r.URL.Path, "/responses") {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	n := s.n
	s.n++
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	io.WriteString(w, sse(overloadedLead...))
	w.(http.Flusher).Flush()
	select {
	case <-time.After(s.wait):
	case <-r.Context().Done():
		return
	}
	io.WriteString(w, s.then(n))
}

// What only frames a reply, or isn't known, doesn't let a stream through;
// text, reasoning, a call or a reply's end does.
func TestStreamEventUnknownOnlyFrames(t *testing.T) {
	for _, x := range []struct {
		ev   string
		kind int
	}{
		{`data: {"type":"keepalive"}`, eventLead},
		{`event: response.rate_limits.updated` + "\n" + `data: {"type":"response.rate_limits.updated"}`, eventLead},
		{`data: {"type":"response.output_item.added","item":{"type":"compaction_summary"}}`, eventLead},
		{`data: {"x":1}`, eventLead},
		{`data: {}`, eventLead},
		{`data: {"type":"response.output_item.added","item":{"type":"web_search_call","id":"ws"}}`, eventContent},
		{`data: {"type":"response.output_item.added","item":{"type":"mcp_list_tools"}}`, eventContent},
		{`data: {"type":"response.web_search_call.searching","item_id":"ws"}`, eventContent},
		{`data: {"type":"response.function_call_arguments.delta","delta":"{"}`, eventContent},
		{`data: {"type":"response.custom_tool_call_input.delta","delta":"x"}`, eventContent},
		{`data: {"type":"response.output_text.annotation.added"}`, eventContent},
		{`data: {"type":"response.refusal.delta","delta":"no"}`, eventContent},
		{`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"s","name":"web_search"}}`, eventContent},
		{`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"container_upload"}}`, eventLead},
		{`event: message_stop` + "\n" + `data: {"type":"message_stop"}`, eventContent},
		{`data: [DONE]`, eventContent},
	} {
		if k, _, _ := streamEvent([]byte(x.ev)); k != x.kind {
			t.Errorf("streamEvent(%s) = %d, want %d", x.ev, k, x.kind)
		}
	}
}
