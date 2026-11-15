package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// cloudflarePage is a page a relay serves 200 text/html in place of a
// reply (#1012): Cloudflare's challenge, as it begins.
const cloudflarePage = `<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title><meta http-equiv="Content-Type" content="text/html; charset=UTF-8"></head><body><div class="main-wrapper" role="main"><div class="main-content"><noscript>Enable JavaScript and cookies to continue</noscript></div></div></body></html>`

// notAPIOn saves a provider speaking proto at s, its only model m.
func notAPIOn(t *testing.T, id string, proto provider.Protocol, s *scripted) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(s)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Models: []string{"m"}}
	switch proto {
	case provider.Anthropic:
		p.Anthropic = up.URL
	case provider.Responses:
		p.Responses = up.URL + "/v1"
	default:
		p.Chat = up.URL + "/v1"
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return up
}

// goodReply is a whole reply saying "from b", streamed or not, in proto.
func goodReply(proto provider.Protocol, stream bool) reply {
	switch {
	case proto == provider.Chat && !stream:
		return reply{200, "application/json", `{"id":"ok","choices":[{"index":0,"message":{"role":"assistant","content":"from b"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`}
	case proto == provider.Chat:
		return reply{200, "text/event-stream", sse(`data: {"id":"ok","choices":[{"index":0,"delta":{"role":"assistant","content":"from b"}}]}`,
			`data: {"id":"ok","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
			`data: [DONE]`)}
	case proto == provider.Responses && !stream:
		return reply{200, "application/json", `{"id":"resp_b","object":"response","status":"completed","model":"m","output":[{"type":"message","id":"msg_b","role":"assistant","status":"completed","content":[{"type":"output_text","text":"from b","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`}
	case proto == provider.Responses:
		return reply{200, "text/event-stream", sse(`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"resp_b","status":"in_progress","output":[]}}`,
			`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","item_id":"msg_b","output_index":0,"content_index":0,"delta":"from b"}`,
			`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"resp_b","status":"completed","model":"m","output":[{"type":"message","id":"msg_b","role":"assistant","status":"completed","content":[{"type":"output_text","text":"from b","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`)}
	case !stream:
		return reply{200, "application/json", `{"id":"msg_b","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"from b"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`}
	}
	return reply{200, "text/event-stream", sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_b","role":"assistant","content":[],"usage":{"input_tokens":3}}}`,
		`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from b"}}`,
		`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
		`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
}

// askOn sends a request for model in proto, streamed or not.
func askOn(s *Server, proto provider.Protocol, model string, stream bool) *httptest.ResponseRecorder {
	st := "false"
	if stream {
		st = "true"
	}
	var path, body string
	switch proto {
	case provider.Responses:
		path, body = "/v1/responses", `{"model":"`+model+`","stream":`+st+`,"input":[{"role":"user","content":"hi"}]}`
	case provider.Anthropic:
		path, body = "/v1/messages", `{"model":"`+model+`","max_tokens":10,"stream":`+st+`,"messages":[{"role":"user","content":"hi"}]}`
	default:
		path, body = "/v1/chat/completions", `{"model":"`+model+`","stream":`+st+`,"messages":[{"role":"user","content":"hi"}]}`
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec
}

// A provider with nobody after it hands the agent a 502 saying what came
// back, not the page as a reply, and the log has it as the error it was.
func TestNotAnAPIReplyAlone(t *testing.T) {
	fresh(t)
	a := &scripted{replies: []reply{{200, "text/html; charset=utf-8", cloudflarePage}}}
	notAPIOn(t, "a", provider.Responses, a)
	s := New()
	rec := askOn(s, provider.Responses, "a/m", true)
	body := rec.Body.String()
	if strings.Contains(body, "<html") || !strings.Contains(body, "A: answered 200 OK with a web page (text/html), not an API reply: “Just a moment...”") {
		t.Fatalf("%d %s", rec.Code, body)
	}
	calls := s.Recent()
	if len(calls) == 0 || calls[0].Status != http.StatusBadGateway || !strings.Contains(calls[0].Error, "with a web page") {
		t.Fatalf("log: %+v", calls)
	}
}

// What an API does answer goes as it came: a reply that begins with
// whitespace, a stream, a compressed body, and a 204.
func TestAPIReplyUntouched(t *testing.T) {
	for _, x := range []struct {
		ct, ce, body string
		code         int
	}{
		{"application/json", "", "\n  {\"ok\":true}", 200},
		{"text/event-stream", "", "data: {}\n\n", 200},
		{"", "", "data: {}\n\n", 200},
		{"text/plain", "", "event: x\ndata: {}\n\n", 200},
		{"application/json", "gzip", "\x1f\x8b\x08", 200},
		{"application/xml", "", "<?xml version=\"1.0\"?><a/>", 200},
		{"application/json", "", "", 204},
		{"text/html", "", "<html></html>", 404},
	} {
		res := &http.Response{StatusCode: x.code, Status: http.StatusText(x.code), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(x.body))}
		if x.ct != "" {
			res.Header.Set("Content-Type", x.ct)
		}
		if x.ce != "" {
			res.Header.Set("Content-Encoding", x.ce)
		}
		got := notAnAPIReply(res, "")
		raw, _ := io.ReadAll(got.Body)
		b := string(raw)
		if got.StatusCode != x.code || b != x.body {
			t.Errorf("%+v: %d %q", x, got.StatusCode, b)
		}
	}
}

// Codex's own sign-in, relayed to the ChatGPT backend, is told of a web
// page served 200 as the 502 it is, not handed the page as its stream.
func TestCodexBackendWebPage(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, cloudflarePage)
	})
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":[]}`))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "<html") || !strings.Contains(rec.Body.String(), "OpenAI: answered 200 OK with a web page") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
