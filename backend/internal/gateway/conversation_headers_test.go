package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// withHeaders gives the saved provider id the headers.
func withHeaders(t *testing.T, id string, h map[string]string) {
	t.Helper()
	p, err := provider.Find(id)
	if err != nil {
		t.Fatal(err)
	}
	p.Headers = h
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
}

// Axel Burks on Discord: a gateway in front of several Azure accounts
// routes one conversation to one account by a session id in a header,
// so its reasoning can be read back. ${sessionId} in a provider's header
// is the conversation's id, as in its extraBody — on every API, streamed
// or whole, relayed or translated, and on count_tokens.
func TestHeadersCarryTheSession(t *testing.T) {
	head := map[string]string{"X-Session-Id": "${sessionId}", "X-Route": "acct/${sessionId}/${threadId}"}
	for _, c := range []struct {
		name, path, body string
		proto            provider.Protocol
		reply, ctype     string
	}{
		{"chat streamed", "/v1/chat/completions", `{"model":"m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`, provider.Chat, chatReply, ""},
		{"responses whole", "/v1/responses", `{"model":"m1","input":"hi"}`, provider.Responses, responsesReply, "application/json"},
		{"messages", "/v1/messages", `{"model":"m1","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, provider.Anthropic, anthropicReply(), ""},
		{"messages to chat", "/v1/messages", `{"model":"m1","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, provider.Chat, chatReply, ""},
		{"count_tokens", "/v1/messages/count_tokens", `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`, provider.Anthropic, `{"input_tokens":3}`, "application/json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{t: t, reply: c.reply, ctype: c.ctype}
			setup(t, c.proto, f)
			withHeaders(t, "fake", head)
			code, body := postWith(t, c.path, c.body, map[string]string{"session_id": "sess-42"})
			if code != 200 {
				t.Fatalf("status %d: %s", code, body)
			}
			if got := f.head.Get("X-Session-Id"); got != "sess-42" {
				t.Errorf("X-Session-Id %q", got)
			}
			if got := f.head.Get("X-Route"); got != "acct/sess-42/sess-42" {
				t.Errorf("X-Route %q", got)
			}
		})
	}
}

// ${subagentId} in a header is the Claude Code subagent the request is
// of, as in extraBody (upstream #2937): "" on the conversation's own
// turns, never the literal.
func TestHeadersCarryTheSubagent(t *testing.T) {
	f := &fake{t: t, reply: responsesReply, ctype: "application/json"}
	setup(t, provider.Responses, f)
	withHeaders(t, "fake", map[string]string{"X-Route": "${sessionId}/${subagentId}"})
	for agent, want := range map[string]string{"": "sess-1/", "worker-a": "sess-1/worker-a"} {
		head := map[string]string{"x-claude-code-session-id": "sess-1"}
		if agent != "" {
			head["x-claude-code-agent-id"] = agent
		}
		code, body := postWith(t, "/v1/responses", `{"model":"m1","input":"hi"}`, head)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		if got := f.head.Get("X-Route"); got != want {
			t.Errorf("agent %q: X-Route %q, want %q", agent, got, want)
		}
	}
}

// Without a session header, the header's id is the one extraBody gets:
// derived from the first user message, the same every turn, never the
// literal.
func TestHeadersDerivedSessionMatchesExtraBody(t *testing.T) {
	f := &fake{t: t, reply: chatReply}
	setup(t, provider.Chat, f)
	withExtraBody(t, "fake", `{"user":"${sessionId}"}`)
	withHeaders(t, "fake", map[string]string{"X-Session-Id": "${sessionId}"})
	ask := func(msgs string) string {
		t.Helper()
		if code, body := post(t, "/v1/chat/completions", `{"model":"m1","messages":`+msgs+`}`); code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		h := f.head.Get("X-Session-Id")
		if b := gjson.GetBytes(f.got, "user").String(); h != b {
			t.Errorf("header %q, body %q", h, b)
		}
		return h
	}
	one := ask(`[{"role":"user","content":"first"}]`)
	again := ask(`[{"role":"user","content":"first"},{"role":"assistant","content":"OK"},{"role":"user","content":"next"}]`)
	if !strings.HasPrefix(one, "uni-switch-") || one != again {
		t.Errorf("ids %q %q", one, again)
	}
}

// Codex names a thread apart from its session: a subagent shares its
// lead's session_id and has a thread_id of its own. ${threadId} is it, in
// headers and extraBody; ${sessionId} stays the session's.
func TestThreadIDIsCodexThread(t *testing.T) {
	f := &fake{t: t, reply: responsesReply, ctype: "application/json"}
	setup(t, provider.Responses, f)
	withExtraBody(t, "fake", `{"metadata":{"thread":"${threadId}"}}`)
	withHeaders(t, "fake", map[string]string{"X-Session-Id": "${sessionId}", "X-Thread-Id": "${threadId}"})
	for _, c := range []struct {
		name string
		head map[string]string
		want string
	}{
		{"turn metadata", map[string]string{"session_id": "root-1", "x-codex-turn-metadata": `{"session_id":"root-1","thread_id":"worker-1"}`}, "worker-1"},
		{"thread-id header", map[string]string{"session-id": "root-1", "thread-id": "worker-2"}, "worker-2"},
		{"none named", map[string]string{"session_id": "root-1"}, "root-1"},
	} {
		code, body := postWith(t, "/v1/responses", `{"model":"m1","input":"hi"}`, c.head)
		if code != 200 {
			t.Fatalf("%s: status %d: %s", c.name, code, body)
		}
		if s, th, b := f.head.Get("X-Session-Id"), f.head.Get("X-Thread-Id"), gjson.GetBytes(f.got, "metadata.thread").String(); s != "root-1" || th != c.want || b != c.want {
			t.Errorf("%s: session %q, thread %q, body %q", c.name, s, th, b)
		}
	}
}

// A session id is the agent's: a CR or LF in it never reaches a header,
// where it would split it or fail the request.
func TestHeaderSessionWithoutCRLF(t *testing.T) {
	f := &fake{t: t, reply: chatReply}
	setup(t, provider.Chat, f)
	withHeaders(t, "fake", map[string]string{"X-Session-Id": "${sessionId}"})
	code, body := postWith(t, "/v1/chat/completions", `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`, map[string]string{"x-session-id": "a\r\nX-Evil: 1"})
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if got := f.head.Get("X-Session-Id"); got != "aX-Evil: 1" || f.head.Get("X-Evil") != "" {
		t.Errorf("X-Session-Id %q, headers %q", got, f.head)
	}
}
