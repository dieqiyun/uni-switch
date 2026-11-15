package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The Cursor plugin's step that collected an earlier step's late usage
// event (upstream #1053) tells it apart, as uni_switch_deferred: the
// step's prompt_tokens stay its own, which a client reads as how full its
// context is (pi read 366940 = 181270 + 185670 of a 300k window, where
// 185670 was used), and uni-switch bills both.
const deferredJSON = `{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":185670,"completion_tokens":2240,"total_tokens":187910,"prompt_tokens_details":{"cached_tokens":181266,"cache_write_tokens":4400},"uni_switch_deferred":{"prompt_tokens":181270,"completion_tokens":1913,"cached_tokens":177533,"cache_write_tokens":3735}}}`

var deferredStream = "data: " + deferredJSON + "\n\ndata: [DONE]\n\n"

func TestSniffChatDeferredUsage(t *testing.T) {
	s := newSniffer(provider.Chat, "text/event-stream")
	s.write([]byte(deferredStream))
	got := s.usage()
	if d := got.Deferred; d == nil || *d != (Usage{Input: 2, Output: 1913, CacheRead: 177533, CacheWrite: 3735}) {
		t.Fatalf("deferred %+v", got.Deferred)
	}
	got.Deferred = nil
	if own := (Usage{Input: 4, Output: 2240, CacheRead: 181266, CacheWrite: 4400, ResponseID: "c", Stop: "tool_calls"}); got != own {
		t.Fatalf("own %+v want %+v", got, own)
	}
}

func TestDeferredUsageIsBilledNotPrompt(t *testing.T) {
	s := newSniffer(provider.Chat, "text/event-stream")
	s.write([]byte(deferredStream))
	u := s.usage()
	b := u.billed()
	if b.Deferred != nil || b.Input != 6 || b.Output != 2240+1913 || b.CacheRead != 181266+177533 || b.CacheWrite != 4400+3735 {
		t.Fatalf("billed %+v", b)
	}
	if u.prompt() != 185670 {
		t.Fatalf("the request's own prompt %d", u.prompt())
	}
	rs := routeUsage("cursor", "claude-opus-5-5", u)
	if len(rs) != 2 {
		t.Fatalf("tiers %+v", rs)
	}
	// the route's prompt, as its context is read, is the request's own
	if n := promptCounted(rs); n != 185670 {
		t.Fatalf("promptCounted %d", n)
	}
	var in, out int
	for _, r := range rs {
		in += r.Input + r.CacheRead + r.CacheWrite
		out += r.Output
	}
	if in != 185670+181270 || out != 2240+1913 {
		t.Fatalf("priced %d in %d out", in, out)
	}
}

func TestChatDecoderKeepsDeferredUsage(t *testing.T) {
	d := &chatDecoder{}
	var u Usage
	for _, line := range []string{`{"id":"c","choices":[{"index":0,"delta":{"content":""}}]}`, deferredJSON} {
		if err := d.decode(line, func(ev Event) {
			if ev.Kind == KUsage {
				u.add(ev.Usage)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	if u.prompt() != 185670 || u.Deferred == nil || u.Deferred.prompt() != 181270 {
		t.Fatalf("usage %+v deferred %+v", u, u.Deferred)
	}
	// a client of another API is told the request's own, as before
	if c := u.chat(); c["prompt_tokens"] != 185670 {
		t.Fatalf("told %v", c)
	}
}

func TestNoDeferredUsageAsBefore(t *testing.T) {
	s := newSniffer(provider.Chat, "text/event-stream")
	s.write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"prompt_tokens_details\":{\"cached_tokens\":60}}}\n\n"))
	u := s.usage()
	if u.Deferred != nil || u.billed() != u || len(routeUsage("p", "m", u)) != 1 {
		t.Fatalf("%+v", u)
	}
}

// a real step of the Cursor plugin, its tool steps' events collected late
// (STEP_WAIT shortened): its own prompt 1371, and 2619 of earlier steps
const realDeferredChunk = `{"id":"chatcmpl-71dd4976e7f654f774cd7619","object":"chat.completion.chunk","created":1791866687,"model":"composer-2.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1371,"completion_tokens":48,"total_tokens":1419,"prompt_tokens_details":{"cached_tokens":1328,"cache_write_tokens":0},"uni_switch_deferred":{"prompt_tokens":2619,"completion_tokens":106,"cached_tokens":2533,"cache_write_tokens":0}}}`

// Claude Code on Cursor: the chat reply is translated, and Claude Code is
// told the step's own prompt, while uni-switch bills the earlier steps' too
func TestTranslatedReplyTellsOwnUsageBillsDeferred(t *testing.T) {
	d := &chatDecoder{}
	var c collector
	if err := d.decode(realDeferredChunk, c.add); err != nil {
		t.Fatal(err)
	}
	res := c.finish()
	if res.Usage.prompt() != 1371 || res.Usage.Deferred == nil || res.Usage.Deferred.prompt() != 2619 || res.Usage.Deferred.Output != 106 {
		t.Fatalf("usage %+v deferred %+v", res.Usage, res.Usage.Deferred)
	}
	a := res.Usage.anthropic()
	if a.InputTokens+a.CacheReadInputTokens+a.CacheCreationInputTokens != 1371 || a.OutputTokens != 48 {
		t.Fatalf("told Claude Code %+v", a)
	}
	if b := res.Usage.billed(); b.prompt() != 1371+2619 || b.Output != 48+106 {
		t.Fatalf("billed %+v", b)
	}
}

// Through the whole gateway: a client is told the request's own usage and
// never uni_switch_deferred's, while the local ledger carries both.
// own 0 is a step whose event showed late: only its deferred is in the reply.
func TestDeferredUsageThroughTheGateway(t *testing.T) {
	const deferred = `"uni_switch_deferred":{"prompt_tokens":181270,"completion_tokens":1913,"cached_tokens":177533,"cache_write_tokens":3735}`
	own := `"usage":{"prompt_tokens":185670,"completion_tokens":2240,"prompt_tokens_details":{"cached_tokens":181266,"cache_write_tokens":4400},` + deferred + `}`
	none := `"usage":{"prompt_tokens":0,"completion_tokens":0,` + deferred + `}`
	chat := func(usage string) string {
		return `{"id":"c","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"tool_calls"}],` + usage + `}`
	}
	stream := func(usage string) string {
		return sse(`data: {"id":"c","model":"m1","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
			`data: {"id":"c","model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+usage+`}`, "data: [DONE]")
	}
	for _, c := range []struct {
		name, ctype, reply, path, request string
		in, out, read, write              int
	}{
		{"chat reply", "application/json", chat(own), "/v1/chat/completions", `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`, 6, 4153, 358799, 8135},
		{"chat stream", "", stream(own), "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`, 6, 4153, 358799, 8135},
		{"translated for Claude Code", "", stream(own), "/v1/messages", `{"model":"m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, 6, 4153, 358799, 8135},
		{"own is 0", "application/json", chat(none), "/v1/chat/completions", `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`, 2, 1913, 177533, 3735},
		{"own is 0, streamed", "", stream(none), "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`, 2, 1913, 177533, 3735},
	} {
		t.Run(c.name, func(t *testing.T) {
			setup(t, provider.Chat, &fake{ctype: c.ctype, reply: c.reply})
			code, body := post(t, c.path, c.request)
			if code != 200 {
				t.Fatalf("gateway: %d %s", code, body)
			}
			if c.path == "/v1/messages" && strings.Contains(body, "uni_switch_deferred") {
				t.Errorf("Claude Code was told uni_switch_deferred: %s", body)
			}
			if r := lastUsage(t); r.Input != c.in || r.Output != c.out || r.CacheRead != c.read || r.CacheWrite != c.write {
				t.Errorf("ledger %+v", r)
			}
		})
	}
}

// a step with nothing of its own and an earlier step's usage still tells
// the gateway (chatDecoder gave up on a usage of 0 prompt and 0 completion)
func TestChatDecoderKeepsDeferredUsageWithNoOwn(t *testing.T) {
	d := &chatDecoder{}
	var got []Usage
	line := `{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"uni_switch_deferred":{"prompt_tokens":1148,"completion_tokens":50,"cached_tokens":1100,"cache_write_tokens":0}}}`
	if err := d.decode(line, func(ev Event) {
		if ev.Kind == KUsage {
			got = append(got, ev.Usage)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].prompt() != 0 || got[0].Deferred == nil || got[0].Deferred.prompt() != 1148 || got[0].Deferred.Output != 50 {
		t.Fatalf("usage %+v", got)
	}
}
