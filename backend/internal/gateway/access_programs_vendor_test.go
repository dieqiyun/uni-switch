package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// programsUpstream stands in for every upstream a test names, by the
// RoundTrip of the gateway's client: nothing reaches the network. It keeps
// what each was sent and answers in the API the path asks.
type programsUpstream struct {
	mu   sync.Mutex
	urls []string
	sent [][]byte
}

func (u *programsUpstream) RoundTrip(r *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.urls = append(u.urls, r.URL.String())
	u.sent = append(u.sent, b)
	u.mu.Unlock()
	var reply string
	switch {
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		reply = sse(`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
			`data: [DONE]`)
	case strings.HasSuffix(r.URL.Path, "/messages"):
		reply = sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":2,"output_tokens":0}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`,
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)
	default:
		reply = sse(`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r1","model":"m","status":"in_progress"}}`,
			`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"OK"}`,
			`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r1","model":"m","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`)
	}
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
}

func (u *programsUpstream) last(t *testing.T) (string, []byte) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.sent) == 0 {
		t.Fatal("no upstream was asked")
	}
	return u.urls[len(u.urls)-1], u.sent[len(u.sent)-1]
}

// #3172 (csic21, HONGs23, TimWongUp): Codex's access_programs, its Cyber
// program ({"cyber":"standard"}, which a subagent inherits), went to every
// Responses upstream, and OpenCode Go answered it for DeepSeek and GLM
// with 503 "Endpoint is unavailable". Codex itself sends the field only to
// OpenAI (a ChatGPT sign-in, or an API key on its "openai" provider), so it
// now reaches the ChatGPT backend, OpenAI's API, another uni-switch and a relay
// set to take Codex's requests as sent, as it came, and no one else, on
// every path to a Responses upstream: relayed, and built from Chat.
func TestAccessProgramsGoToOpenAIAlone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const programs = `{"cyber":"standard"}`
	save := func(p provider.Provider, api string) {
		t.Helper()
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		for _, m := range p.Models {
			if err := provider.SetModelAPI(p.ID+"/"+m, api); err != nil {
				t.Fatal(err)
			}
		}
	}
	save(provider.Provider{ID: "ocgo", Name: "OpenCode Go", Key: "k",
		Chat: "https://opencode.ai/zen/go/v1", Responses: "https://opencode.ai/zen/go/v1",
		Models: []string{"deepseek-v4.1-flash", "glm-5.3-flash"}}, "responses")
	save(provider.Provider{ID: "ocgo-extra", Name: "OpenCode Go, the user's own program", Key: "k",
		Responses: "https://opencode.ai/zen/go/v1", Models: []string{"deepseek-v4.1-flash"},
		ExtraBody: provider.ExtraBody{"access_programs": json.RawMessage(`{"cyber":"users_own"}`)}}, "responses")
	save(provider.Provider{ID: "oczen", Name: "OpenCode Zen", Key: "k",
		Responses: "https://opencode.ai/zen/v1", Models: []string{"deepseek-v4.1-flash"}}, "responses")
	save(provider.Provider{ID: "relay", Name: "Relay", Key: "k",
		Responses: "https://relay.test/v1", Models: []string{"gpt-5.5"}}, "responses")
	save(provider.Provider{ID: "asis", Name: "Relay as sent", Key: "k", CodexAsSent: true,
		Responses: "https://asis.test/v1", Models: []string{"gpt-5.5"}}, "responses")
	save(provider.Provider{ID: "oai", Name: "OpenAI", Key: "sk-k",
		Responses: "https://api.openai.com/v1", Models: []string{"gpt-5.5"}}, "responses")
	save(provider.Provider{ID: "chatonly", Name: "Chat only", Key: "k",
		Chat: "https://chat.test/v1", Models: []string{"deepseek-chat"}}, "chat")
	save(provider.Provider{ID: "msgonly", Name: "Messages only", Key: "k",
		Anthropic: "https://messages.test", Models: []string{"glm-5.3"}}, "anthropic")

	up := &programsUpstream{}
	s := New()
	s.client = &http.Client{Transport: up}
	codex := http.Header{"User-Agent": {"codex_cli_rs/0.150.0 (Mac OS 26.2.0; arm64)"}, "Originator": {"codex_cli_rs"}}

	resp := func(model, ap string) string {
		return `{"model":"` + model + `","input":"Reply with OK.","stream":true,"reasoning":{"effort":"high"},"access_programs":` + ap + `}`
	}
	chat := func(model string) string {
		return `{"model":"` + model + `","messages":[{"role":"user","content":"Reply with OK."}],"stream":true,"access_programs":` + programs + `}`
	}
	for _, c := range []struct {
		name, path, body string
		h                http.Header
		want             string // the access_programs sent, "" for none
	}{
		// the reporters' requests: Codex's subagent, and their curl
		{"go deepseek, codex", "/v1/responses", resp("ocgo/deepseek-v4.1-flash", programs), codex, ""},
		{"go glm, codex", "/v1/responses", resp("ocgo/glm-5.3-flash", programs), codex, ""},
		{"go deepseek, curl", "/v1/responses", resp("ocgo/deepseek-v4.1-flash", programs), nil, ""},
		{"go, null", "/v1/responses", resp("ocgo/deepseek-v4.1-flash", "null"), codex, ""},
		{"go, chat built as responses", "/v1/chat/completions", chat("ocgo/deepseek-v4.1-flash"), nil, ""},
		// nobody else Codex would send it to
		{"zen", "/v1/responses", resp("oczen/deepseek-v4.1-flash", programs), codex, ""},
		{"relay", "/v1/responses", resp("relay/gpt-5.5", programs), codex, ""},
		{"relay as sent, not from codex", "/v1/responses", resp("asis/gpt-5.5", programs), nil, ""},
		// a value the user set for the provider goes, over the client's
		{"go, the user's extraBody", "/v1/responses", resp("ocgo-extra/deepseek-v4.1-flash", programs), codex, `{"cyber":"users_own"}`},
		// what Codex sends OpenAI goes to OpenAI as it came
		{"openai", "/v1/responses", resp("oai/gpt-5.5", `{"cyber":"daybreak_blue"}`), codex, `{"cyber":"daybreak_blue"}`},
		{"openai, null", "/v1/responses", resp("oai/gpt-5.5", "null"), codex, "null"},
		{"openai, chat built as responses", "/v1/chat/completions", chat("oai/gpt-5.5"), nil, programs},
		{"relay as sent, from codex", "/v1/responses", resp("asis/gpt-5.5", programs), codex, programs},
		// no other API has the field
		{"chat upstream", "/v1/responses", resp("chatonly/deepseek-chat", programs), codex, ""},
		{"messages upstream", "/v1/responses", resp("msgonly/glm-5.3", programs), codex, ""},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		for k, vs := range c.h {
			req.Header[k] = vs
		}
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		u, sent := up.last(t)
		ap := gjson.GetBytes(sent, "access_programs")
		if c.want == "" && ap.Exists() || c.want != "" && ap.Raw != c.want {
			t.Errorf("%s: %s was sent access_programs %q, want %q, in %s", c.name, u, ap.Raw, c.want, sent)
		}
		if strings.Count(string(sent), `"access_programs"`) > 1 {
			t.Errorf("%s: access_programs sent twice: %s", c.name, sent)
		}
	}
}

func TestWithoutTopMember(t *testing.T) {
	big := `{"input":"` + strings.Repeat("a", validMemoMin) + `", "access_programs":{"cyber":"standard"},"model":"m"}`
	for _, c := range []struct{ name, body, want string }{
		{"middle", `{"model":"m", "access_programs": {}, "input":"hi"}`, `{"model":"m", "input":"hi"}`},
		{"first", `{"access_programs":{"cyber":"standard"},"model":"m"}`, `{"model":"m"}`},
		{"last", `{"model":"m", "access_programs": null}`, `{"model":"m"}`},
		{"only", `{"access_programs":{"cyber":"standard"}}`, `{}`},
		{"twice", `{"access_programs":1,"x":2,"access_programs":3,"y":4}`, `{"x":2,"y":4}`},
		{"twice in a row, last", `{"x":2,"access_programs":1,"access_programs":3}`, `{"x":2}`},
		{"escaped key", `{"access_programs":1,"x":2}`, `{"x":2}`},
		{"nested kept", `{"x":{"access_programs":1},"access_programs":2}`, `{"x":{"access_programs":1}}`},
		{"spaces around", "\n {\"model\":\"m\", \"access_programs\":{}}\n", "\n {\"model\":\"m\"}\n"},
		{"long body", big, `{"input":"` + strings.Repeat("a", validMemoMin) + `", "model":"m"}`},
		{"absent", `{"model":"m"}`, `{"model":"m"}`},
		{"not an object", `["access_programs"]`, `["access_programs"]`},
		{"cut short", `{"model":"m", "access_programs":`, `{"model":"m", "access_programs":`},
	} {
		if got := string(withoutTopMember([]byte(c.body), "access_programs")); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
