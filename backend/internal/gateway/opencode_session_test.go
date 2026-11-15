package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// openCodeMissing is what OpenCode's gateway answers a request that names
// no session (luci on Discord).
const openCodeMissing = `{"type":"error","error":{"type":"invalid_request_error","message":"Request is missing x-opencode-session and cannot be routed efficiently"}}`

// fakeOpenCode answers as OpenCode's gateway does: 400 openCodeMissing
// without x-opencode-session, a Responses or chat reply with it. It keeps
// the session each request named, by its host, "" for none.
type fakeOpenCode struct {
	mu       sync.Mutex
	sessions map[string][]string
}

func (f *fakeOpenCode) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
	s := r.Header.Get("x-opencode-session")
	f.mu.Lock()
	f.sessions[r.URL.Host] = append(f.sessions[r.URL.Host], s)
	f.mu.Unlock()
	h := http.Header{"Content-Type": {"application/json"}}
	if s == "" && strings.Contains(r.URL.Host, "opencode.ai") {
		return &http.Response{StatusCode: 400, Header: h, Body: io.NopCloser(strings.NewReader(openCodeMissing)), Request: r}, nil
	}
	reply := `{"id":"c","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	if strings.HasSuffix(r.URL.Path, "/responses") {
		reply = `{"id":"resp_1","object":"response","status":"completed","model":"glm-5.3","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
}

func (f *fakeOpenCode) got(host string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sessions[host]...)
}

// luci on Discord: OpenCode Go, added as a custom provider and asked from
// Codex, answered 400 "Request is missing x-opencode-session and cannot
// be routed efficiently", and a session set on the provider by hand put
// every conversation on one backend. Every request to OpenCode's gateway
// names its conversation's session: the turns of one conversation the
// same, two conversations two — whether the client names its session
// (Codex's session_id) or only its thread (prompt_cache_key), and
// whatever port the provider's URL gives. Nobody else is sent one.
func TestOpenCodeGoSessionPerConversation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []provider.Provider{
		{ID: "ocgo", Name: "OpenCode Go", Key: "k", Responses: "https://opencode.ai/zen/go/v1", Models: []string{"glm-5.3"}},
		{ID: "ocport", Name: "OpenCode Go 443", Key: "k", Chat: "https://opencode.ai:443/zen/go/v1", Models: []string{"glm-5.3"}},
		{ID: "other", Name: "Other", Key: "k", Responses: "https://other.test/v1", Models: []string{"glm-5.3"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeOpenCode{sessions: map[string][]string{}}
	s := New()
	s.client = &http.Client{Transport: f}
	// a Codex turn: its environment message first, the same in every
	// conversation opened in one folder, then the task
	turn := func(path, model, task string, h http.Header, cacheKey string) {
		t.Helper()
		body := `{"model":"` + model + `","input":[{"role":"user","content":"<environment_context><cwd>/w</cwd></environment_context>"},{"role":"user","content":"` + task + `"}]`
		if path == "/v1/chat/completions" {
			body = `{"model":"` + model + `","messages":[{"role":"user","content":"` + task + `"}]`
		}
		if cacheKey != "" {
			body += `,"prompt_cache_key":"` + cacheKey + `"`
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(body+"}"))
		req.Header.Set("User-Agent", "codex_cli_rs/0.130.0 (Mac OS 26.2.0; arm64)")
		req.Header.Set("originator", "codex_cli_rs")
		for k, v := range h {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", model, task, rec.Code, rec.Body)
		}
	}
	// Codex naming its session
	a := http.Header{"Session_id": {"019a0000-0000-7000-8000-00000000000a"}}
	b := http.Header{"Session_id": {"019a0000-0000-7000-8000-00000000000b"}}
	turn("/v1/responses", "ocgo/glm-5.3", "task a", a, "")
	turn("/v1/responses", "ocgo/glm-5.3", "task a, turn 2", a, "")
	turn("/v1/responses", "ocgo/glm-5.3", "task b", b, "")
	// Codex naming only its thread, in the body
	turn("/v1/responses", "ocgo/glm-5.3", "task c", nil, "thread-c")
	turn("/v1/responses", "ocgo/glm-5.3", "task c, turn 2", nil, "thread-c")
	turn("/v1/responses", "ocgo/glm-5.3", "task d", nil, "thread-d")
	// the client's own x-opencode-session, in OpenCode's form, goes as it is
	turn("/v1/responses", "ocgo/glm-5.3", "task e", http.Header{"X-Opencode-Session": {"ses_ecc5d8f28ffe9Cy14V8Em7lI2u"}}, "")
	got := f.got("opencode.ai")
	if len(got) != 7 {
		t.Fatalf("requests to opencode.ai: %q", got)
	}
	for i, v := range got {
		if !strings.HasPrefix(v, "ses_") {
			t.Fatalf("request %d named session %q", i, v)
		}
	}
	if got[0] != got[1] || got[0] == got[2] {
		t.Errorf("session_id: conversation a %q %q, b %q", got[0], got[1], got[2])
	}
	if got[3] != got[4] || got[3] == got[5] || got[3] == got[0] || got[5] == got[2] {
		t.Errorf("prompt_cache_key: thread c %q %q, d %q (a %q, b %q)", got[3], got[4], got[5], got[0], got[2])
	}
	if got[6] != "ses_ecc5d8f28ffe9Cy14V8Em7lI2u" {
		t.Errorf("the client's own session went as %q", got[6])
	}

	// a provider whose URL names OpenCode's port
	turn("/v1/chat/completions", "ocport/glm-5.3", "task p", a, "")
	turn("/v1/chat/completions", "ocport/glm-5.3", "task p, turn 2", a, "")
	if p := f.got("opencode.ai:443"); len(p) != 2 || p[0] == "" || p[0] != p[1] {
		t.Errorf("opencode.ai:443: %q", p)
	}

	turn("/v1/responses", "other/glm-5.3", "task o", a, "")
	if o := f.got("other.test"); len(o) != 1 || o[0] != "" {
		t.Errorf("another vendor was sent x-opencode-session: %q", o)
	}
}
