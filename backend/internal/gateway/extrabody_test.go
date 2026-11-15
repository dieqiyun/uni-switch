package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// withExtraBody gives the saved provider id the extraBody written.
func withExtraBody(t *testing.T, id, extra string) {
	t.Helper()
	p, err := provider.Find(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(extra), &p.ExtraBody); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
}

// postWith is post with the client's own headers.
func postWith(t *testing.T, path, body string, head map[string]string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	for k, v := range head {
		req.Header.Set(k, v)
	}
	New().Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

const (
	chatReply      = "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	responsesReply = `{"id":"r","object":"response","status":"completed","output":[]}`
)

func anthropicReply() string {
	return sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m1","usage":{"input_tokens":7}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`data: {"type":"message_stop"}`)
}

// Luc-xz's relay (#1604) turns a Responses request away without a
// prompt_cache_key: the provider's extraBody adds one, the session's id,
// filling in only what the client didn't send, unless a name is forced.
func TestExtraBodyResponsesRelayed(t *testing.T) {
	f := &fake{t: t, reply: responsesReply, ctype: "application/json"}
	setup(t, provider.Responses, f)
	withExtraBody(t, "fake", `{"prompt_cache_key":"${sessionId}","metadata":{"who":"agent-${sessionId}"},"!service_tier":"flex","model":"other"}`)

	code, body := postWith(t, "/v1/responses", `{"model":"m1","input":"hi","service_tier":"auto"}`, map[string]string{"session_id": "sess-42"})
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if got := gjson.GetBytes(f.got, "prompt_cache_key").String(); got != "sess-42" {
		t.Errorf("prompt_cache_key %q in %s", got, f.got)
	}
	if got := gjson.GetBytes(f.got, "metadata.who").String(); got != "agent-sess-42" {
		t.Errorf("metadata.who %q in %s", got, f.got)
	}
	if got := gjson.GetBytes(f.got, "service_tier").String(); got != "flex" {
		t.Errorf("forced service_tier %q in %s", got, f.got)
	}
	if got := gjson.GetBytes(f.got, "model").String(); got != "m1" {
		t.Errorf("model %q in %s", got, f.got)
	}
	if gjson.GetBytes(f.got, "input").String() != "hi" {
		t.Errorf("input changed: %s", f.got)
	}

	// the client's own key is kept
	code, body = postWith(t, "/v1/responses", `{"model":"m1","input":"hi","prompt_cache_key":"mine"}`, map[string]string{"session_id": "sess-42"})
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if got := gjson.GetBytes(f.got, "prompt_cache_key").String(); got != "mine" {
		t.Errorf("client's prompt_cache_key replaced by %q", got)
	}
}

// Claude Code's subagents share the conversation's session header, so
// ${sessionId} alone puts every subagent's turns on one prompt_cache_key
// (upstream #2937), more than a relay keeps warm. ${subagentId} is
// the request's x-claude-code-agent-id, which the conversation's own
// turns never send: the lead's key stays one shard, each subagent's
// turns another.
func TestExtraBodySubagentId(t *testing.T) {
	f := &fake{t: t, reply: responsesReply, ctype: "application/json"}
	setup(t, provider.Responses, f)
	withExtraBody(t, "fake", `{"prompt_cache_key":"${sessionId}:${subagentId}"}`)

	ask := func(agent string) string {
		t.Helper()
		head := map[string]string{"x-claude-code-session-id": "sess-1"}
		if agent != "" {
			head["x-claude-code-agent-id"] = agent
		}
		code, body := postWith(t, "/v1/responses", `{"model":"m1","input":"hi"}`, head)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		return gjson.GetBytes(f.got, "prompt_cache_key").String()
	}
	if got := ask(""); got != "sess-1:" {
		t.Errorf("the lead's key %q", got)
	}
	if got := ask("worker-a"); got != "sess-1:worker-a" {
		t.Errorf("a subagent's key %q", got)
	}
	if got := ask("worker-b"); got == "sess-1:worker-a" || got == "sess-1:" {
		t.Errorf("another subagent's key %q", got)
	}
}

// Without a session header, ${sessionId} is the id uni-switch derives from the
// conversation's first user message: never the literal, the same on every
// turn of one conversation, different for another.
func TestExtraBodyChatDerivedSession(t *testing.T) {
	f := &fake{t: t, reply: chatReply}
	setup(t, provider.Chat, f)
	withExtraBody(t, "fake", `{"user":"${sessionId}","temperature":0.5}`)

	ask := func(msgs string) string {
		t.Helper()
		code, body := post(t, "/v1/chat/completions", `{"model":"m1","messages":`+msgs+`}`)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		if gjson.GetBytes(f.got, "temperature").Raw != "0.5" {
			t.Errorf("temperature in %s", f.got)
		}
		return gjson.GetBytes(f.got, "user").String()
	}
	one := ask(`[{"role":"user","content":"first"}]`)
	again := ask(`[{"role":"user","content":"first"},{"role":"assistant","content":"OK"},{"role":"user","content":"next"}]`)
	other := ask(`[{"role":"user","content":"another"}]`)
	if !strings.HasPrefix(one, "uni-switch-") || one != again || one == other {
		t.Errorf("derived ids: %q %q %q", one, again, other)
	}
}

// A request uni-switch translates gets the fields too, on top of the
// translated body: Messages to a Chat provider, Chat to a Responses one.
func TestExtraBodyTranslated(t *testing.T) {
	t.Run("messages to chat", func(t *testing.T) {
		f := &fake{t: t, reply: chatReply}
		setup(t, provider.Chat, f)
		withExtraBody(t, "fake", `{"prompt_cache_key":"${sessionId}"}`)
		code, body := postWith(t, "/v1/messages", `{"model":"m1","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, map[string]string{"X-Claude-Code-Session-Id": "cc-1"})
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		if f.path != "/v1/chat/completions" || gjson.GetBytes(f.got, "prompt_cache_key").String() != "cc-1" {
			t.Errorf("%s: %s", f.path, f.got)
		}
	})
	t.Run("chat to responses", func(t *testing.T) {
		f := &fake{t: t, reply: sse(`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
		setup(t, provider.Responses, f)
		withExtraBody(t, "fake", `{"prompt_cache_key":"${sessionId}"}`)
		code, body := postWith(t, "/v1/chat/completions", `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`, map[string]string{"x-session-id": "s-9"})
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		if f.path != "/v1/responses" || gjson.GetBytes(f.got, "prompt_cache_key").String() != "s-9" {
			t.Errorf("%s: %s", f.path, f.got)
		}
	})
}

// An Anthropic provider gets them on Messages, and not on count_tokens,
// which asks for no reply.
func TestExtraBodyAnthropic(t *testing.T) {
	f := &fake{t: t, reply: anthropicReply()}
	setup(t, provider.Anthropic, f)
	withExtraBody(t, "fake", `{"metadata":{"user_id":"${sessionId}"},"!max_tokens":99}`)
	code, body := postWith(t, "/v1/messages", `{"model":"m1","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, map[string]string{"session_id": "a-1"})
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if gjson.GetBytes(f.got, "metadata.user_id").String() != "a-1" || gjson.GetBytes(f.got, "max_tokens").Int() != 99 {
		t.Errorf("messages body %s", f.got)
	}

	f.reply, f.ctype = `{"input_tokens":3}`, "application/json"
	code, body = postWith(t, "/v1/messages/count_tokens", `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`, map[string]string{"session_id": "a-1"})
	if code != 200 {
		t.Fatalf("count_tokens %d: %s", code, body)
	}
	if f.path != "/v1/messages/count_tokens" || gjson.GetBytes(f.got, "metadata").Exists() {
		t.Errorf("count_tokens %s: %s", f.path, f.got)
	}
}

// A Gemini provider gets them on a Gemini client's relayed request and on
// one translated from Chat.
func TestExtraBodyGemini(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var got []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "gem", Name: "Gem", Key: "AIza-test", Gemini: up.URL + "/v1beta", Models: []string{"gemini-2.5-pro"}}); err != nil {
		t.Fatal(err)
	}
	withExtraBody(t, "gem", `{"cachedContent":"cachedContents/${sessionId}","contents":[]}`)

	code, body := postWith(t, "/v1beta/models/gem/gemini-2.5-pro:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, map[string]string{"session_id": "g-1"})
	if code != 200 {
		t.Fatalf("gemini %d: %s", code, body)
	}
	if gjson.GetBytes(got, "cachedContent").String() != "cachedContents/g-1" || !strings.Contains(gjson.GetBytes(got, "contents").Raw, "hi") {
		t.Errorf("gemini body %s", got)
	}

	code, body = postWith(t, "/v1/chat/completions", `{"model":"gem/gemini-2.5-pro","messages":[{"role":"user","content":"hi"}]}`, map[string]string{"session_id": "g-2"})
	if code != 200 {
		t.Fatalf("chat %d: %s", code, body)
	}
	if gjson.GetBytes(got, "cachedContent").String() != "cachedContents/g-2" {
		t.Errorf("chat to gemini body %s", got)
	}
}
