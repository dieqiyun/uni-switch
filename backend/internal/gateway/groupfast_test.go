package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// fastUp is every vendor at once: the gateway's calls to api.openai.com,
// api.anthropic.com or a relay all come here, and it keeps the last.
type fastUp struct {
	mu                    sync.Mutex
	host, path, beta, raw string
	// refuseTier turns service_tier away as OpenAI does on a model
	// without it; refused counts the times
	refuseTier bool
	refused    int
	calls      int
}

func (u *fastUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.path, u.beta, u.raw = r.URL.Path, r.Header.Get("anthropic-beta"), string(b)
	u.calls++
	refuse := u.refuseTier && strings.Contains(string(b), `"service_tier"`)
	if refuse {
		u.refused++
	}
	u.mu.Unlock()
	if refuse {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'service_tier' is not supported with this model.","param":"service_tier","code":"unsupported_parameter"}}`)
		return
	}
	if strings.Contains(string(b), `"stream":true`) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			io.WriteString(w, sse(
				`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","model":"x","usage":{"input_tokens":1}}}`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
				`data: {"type":"content_block_stop","index":0}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				`data: {"type":"message_stop"}`))
		case strings.HasSuffix(r.URL.Path, "/responses"):
			io.WriteString(w, sse(
				`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r1","model":"x","status":"in_progress","output":[]}}`,
				`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","item_id":"o1","output_index":0,"content_index":0,"delta":"ok"}`,
				`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r1","model":"x","status":"completed","output":[{"type":"message","id":"o1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`))
		default:
			io.WriteString(w, sse(
				`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
				`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
				`data: [DONE]`))
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages"):
		io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	case strings.HasSuffix(r.URL.Path, "/responses"):
		io.WriteString(w, `{"id":"r1","object":"response","status":"completed","model":"x","output":[{"type":"message","id":"o1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	default:
		io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}
}

func (u *fastUp) last(t *testing.T) (host, path, beta string, body map[string]any) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := json.Unmarshal([]byte(u.raw), &body); err != nil {
		t.Fatalf("%v: %q", err, u.raw)
	}
	return u.host, u.path, u.beta, body
}

// fasted is a gateway whose OpenAI key (oa), Anthropic key (an) and relay
// (rl) all answer from one fake vendor, which sees the host each call was
// for.
func fasted(t *testing.T) (*Server, *fastUp) {
	t.Helper()
	fresh(t)
	up := &fastUp{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	to, _ := url.Parse(srv.URL)
	for _, p := range []provider.Provider{
		{ID: "oa", Name: "OpenAI", Key: "ko", Chat: "https://api.openai.com/v1", Responses: "https://api.openai.com/v1", Models: []string{"gpt-6.1-sol", "gpt-6.1-mini"}},
		{ID: "an", Name: "Anthropic", Key: "ka", Anthropic: "https://api.anthropic.com", Models: []string{"claude-opus-5-5", "claude-sonnet-5"}},
		{ID: "rl", Name: "Relay", Key: "kr", Chat: "https://relay.example/v1", Models: []string{"gpt-6.1-sol", "glm-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		up.mu.Lock()
		up.host = r.URL.Host
		up.mu.Unlock()
		r.URL.Scheme, r.URL.Host = to.Scheme, to.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
	return s, up
}

func postProto(t *testing.T, s *Server, path, body string) Route {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	if strings.HasSuffix(path, "/messages") {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	return lastRoute(s)
}

var fastAsks = []struct{ name, path, body string }{
	{"chat", "/v1/chat/completions", `{"model":"group/f","messages":[{"role":"user","content":"hi"}]}`},
	{"responses", "/v1/responses", `{"model":"group/f","input":"hi"}`},
	{"anthropic", "/v1/messages", `{"model":"group/f","max_tokens":32000,"messages":[{"role":"user","content":"hi"}]}`},
}

// The rewrite in each wire format, and only where the vendor has it.
func TestWithFastWire(t *testing.T) {
	for proto, want := range map[string]string{"chat": `"service_tier":"priority"`, "responses": `"service_tier":"priority"`, "anthropic": `"speed":"fast"`} {
		var p provider.Protocol
		switch proto {
		case "chat":
			p = provider.Chat
		case "responses":
			p = provider.Responses
		default:
			p = provider.Anthropic
		}
		if got := string(withFast(p, []byte(`{"model":"m"}`))); !strings.Contains(got, want) {
			t.Errorf("%s: %s", proto, got)
		}
	}
	r, err := parseAnthropic([]byte(`{"model":"m","max_tokens":10,"speed":"fast","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil || !r.Fast {
		t.Fatalf("speed not read: %v %+v", err, r)
	}
	for _, c := range []struct {
		host, model string
		want        bool
	}{
		{"api.anthropic.com", "claude-opus-5-5", true},
		{"api.anthropic.com", "claude-opus-4-8-20260101", true},
		{"api.anthropic.com", "claude-sonnet-5", false},
		{"relay.example", "claude-opus-5-5", false},
	} {
		b, err := build(provider.Anthropic, r, c.model, c.host, false)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Contains(string(b), `"speed":"fast"`)
		if got != c.want {
			t.Errorf("%s %s: speed %v, want %v", c.host, c.model, got, c.want)
		}
	}
	for host, want := range map[string]bool{"api.openai.com": true, "chatgpt.com": true, "relay.example": false} {
		if got := strings.Contains(string(buildResponses(r, "gpt-6.1-sol", host, false)), `"service_tier":"priority"`); got != want {
			t.Errorf("responses %s: tier %v, want %v", host, got, want)
		}
	}
}
