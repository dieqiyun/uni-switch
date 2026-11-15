package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// servedBy is a vendor that answers in its protocol, streamed when asked,
// naming model as the one that answered.
func servedBy(proto provider.Protocol, model string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Stream bool `json:"stream"`
		}
		json.Unmarshal(b, &req)
		m, _ := json.Marshal(model)
		M := string(m)
		if !req.Stream {
			w.Header().Set("Content-Type", "application/json")
			switch proto {
			case provider.Chat:
				io.WriteString(w, `{"id":"c1","object":"chat.completion","model":`+M+`,"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
			case provider.Responses:
				io.WriteString(w, `{"id":"r1","object":"response","model":`+M+`,"status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`)
			default:
				io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":`+M+`,"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`)
			}
			return
		}
		var events []string
		switch proto {
		case provider.Chat:
			events = []string{
				`data: {"id":"c1","model":` + M + `,"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
				`data: {"id":"c1","model":` + M + `,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
				`data: [DONE]`,
			}
		case provider.Responses:
			events = []string{
				"event: response.created\ndata: " + `{"type":"response.created","response":{"id":"r1","model":` + M + `,"status":"in_progress","output":[]}}`,
				"event: response.output_item.added\ndata: " + `{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
				"event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hi"}`,
				"event: response.output_item.done\ndata: " + `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`,
				"event: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"r1","model":` + M + `,"status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`,
			}
		default:
			events = []string{
				"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":` + M + `,"content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
				"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
				"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}`,
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
				"event: message_stop\ndata: " + `{"type":"message_stop"}`,
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			io.WriteString(w, ev+"\n\n")
		}
	}
}

// The model a vendor's reply says answered is kept on the request's
// route, its try and its log entry, whatever the vendor speaks, streamed
// or not, relayed or translated; the route says it was swapped when it is
// another model than the one asked for, and not when it is that model's
// dated name.
func TestServedModelRecorded(t *testing.T) {
	bodies := map[string]string{
		"/v1/messages":         `{"model":"fake/sol","max_tokens":100,"messages":[{"role":"user","content":"hi"}]`,
		"/v1/chat/completions": `{"model":"fake/sol","messages":[{"role":"user","content":"hi"}]`,
		"/v1/responses":        `{"model":"fake/sol","input":"hi"`,
	}
	type path struct {
		name  string
		proto provider.Protocol // the vendor's
		agent string            // the agent's endpoint
	}
	paths := []path{
		{"anthropic", provider.Anthropic, "/v1/messages"},
		{"chat", provider.Chat, "/v1/chat/completions"},
		{"responses", provider.Responses, "/v1/responses"},
		{"chat from anthropic", provider.Anthropic, "/v1/chat/completions"},
		{"anthropic from chat", provider.Chat, "/v1/messages"},
		{"anthropic from responses", provider.Responses, "/v1/messages"},
	}
	for _, pa := range paths {
		for _, stream := range []bool{true, false} {
			for _, c := range []struct {
				served  string
				swapped bool
			}{
				{"luna", true},
				{"sol-2026-01-01", false},
				{"models/SOL", false},
			} {
				name := pa.name + "/" + c.served
				if stream {
					name += "/stream"
				}
				t.Run(name, func(t *testing.T) {
					fresh(t)
					up := httptest.NewServer(servedBy(pa.proto, c.served))
					t.Cleanup(up.Close)
					p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"sol"}}
					switch pa.proto {
					case provider.Chat:
						p.Chat = up.URL + "/v1"
					case provider.Responses:
						p.Responses = up.URL + "/v1"
					case provider.Anthropic:
						p.Anthropic = up.URL
					}
					if err := provider.Save(p); err != nil {
						t.Fatal(err)
					}
					body := bodies[pa.agent]
					if stream {
						body += `,"stream":true`
					}
					s := New()
					rec := httptest.NewRecorder()
					s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", pa.agent, strings.NewReader(body+"}")))
					if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hi") {
						t.Fatalf("%d %s", rec.Code, rec.Body)
					}
					r := lastRoute(s)
					if r.Served != c.served || r.Swapped != c.swapped {
						t.Errorf("route served %q swapped %v; want %q %v", r.Served, r.Swapped, c.served, c.swapped)
					}
					if len(r.Tries) != 1 || r.Tries[0].Model != "sol" || r.Tries[0].Served != c.served || r.Tries[0].Swapped != c.swapped {
						t.Errorf("tries %+v", r.Tries)
					}
					if calls := s.Recent(); len(calls) == 0 || calls[0].Usage.Served != c.served {
						t.Errorf("request log: %+v", calls)
					}
					// the usage ledger has the model asked for, the one sent
					// and the one that answered
					if u := usage.Load(time.Time{}); len(u) != 1 || u[0].Requested != "fake/sol" || u[0].Model != "sol" || u[0].Served != c.served {
						t.Errorf("usage %+v", u)
					}
				})
			}
		}
	}
}

// A reply that names no model marks nothing.
func TestServedModelUnnamed(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"sol"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fake/sol","messages":[{"role":"user","content":"hi"}]}`)))
	if r := lastRoute(s); rec.Code != 200 || r.Served != "" || r.Swapped || r.Tries[0].Swapped {
		t.Fatalf("%d, route %+v", rec.Code, r)
	}
	if u := usage.Load(time.Time{}); len(u) != 1 || u[0].Requested != "fake/sol" || u[0].Served != "" {
		t.Errorf("usage %+v", u)
	}
}

// swapped tells another model from the one asked for, and not the same
// one under its dated, pinned or prefixed name.
func TestSwappedModel(t *testing.T) {
	for _, c := range []struct {
		sent, served string
		want         bool
	}{
		{"sol", "luna", true},
		{"gpt-5", "gpt-5-mini", true},
		{"gpt-5", "gpt-5-mini-2025-08-07", true},
		{"gpt-4o", "gpt-4o-mini", true},
		{"claude-opus-4", "claude-opus-4-1-20250805", true},
		{"deepseek-v3", "deepseek-v2", true},
		{"gpt-5", "gpt-5.1", true},
		{"gpt-5", "gpt-5-2025-08-07", false},
		{"gpt-5", "GPT-5", false},
		{"gpt-4", "gpt-4-0613", false},
		{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929", false},
		{"claude-3-5-sonnet-latest", "claude-3-5-sonnet-20241022", false},
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5", false},
		{"gemini-2.5-pro", "models/gemini-2.5-pro", false},
		{"gemini-1.5-pro", "gemini-1.5-pro-002", false},
		{"gemini-2.5-pro", "gemini-2.5-pro-preview-06-05", false},
		{"openai/gpt-5", "gpt-5-2025-08-07", false},
		{"us.anthropic.claude-sonnet-4-20250514-v1:0", "claude-sonnet-4-20250514", false},
		{"auto", "gpt-5-mini", false}, // Copilot's Auto, Cursor's: the vendor was asked to pick
		{"copilot/auto", "claude-sonnet-5", false},
		// a remote uni-switch's routing group, answered by the member it routed to
		{"group/auto-deepseek-v4-1-flash", "deepseek/deepseek-v4.1-flash", false},
		{"group/fast", "luna", false},
		{"sol", "", false},
		{"", "luna", false},
	} {
		if got := swapped(c.sent, c.served); got != c.want {
			t.Errorf("swapped(%q, %q) = %v, want %v", c.sent, c.served, got, c.want)
		}
	}
}

// The sniffer reads the model a relayed reply names as it goes by.
func TestSniffServedModel(t *testing.T) {
	for _, c := range []struct {
		proto provider.Protocol
		ct    string
		body  string
	}{
		{provider.Chat, "text/event-stream", "data: {\"model\":\"luna\",\"choices\":[]}\n\ndata: [DONE]\n\n"},
		{provider.Chat, "application/json", `{"model":"luna","choices":[]}`},
		{provider.Responses, "text/event-stream", "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"model\":\"luna\"}}\n\n"},
		{provider.Responses, "application/json", `{"object":"response","model":"luna"}`},
		{provider.Anthropic, "text/event-stream", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"luna\"}}\n\n"},
		{provider.Anthropic, "application/json", `{"type":"message","model":"luna"}`},
	} {
		s := newSniffer(c.proto, c.ct)
		s.write([]byte(c.body))
		if got := s.usage().Served; got != "luna" {
			t.Errorf("%s %s: served %q", c.proto, c.ct, got)
		}
	}
}

// A turn on one of Codex's own models, relayed to the ChatGPT backend,
// keeps the model its reply names as the others do.
func TestCodexOwnModelServedTraced(t *testing.T) {
	for _, c := range []struct {
		served  string
		swapped bool
	}{{"gpt-5.4-mini", true}, {"gpt-5.5-2026-04-01", false}} {
		setup(t, provider.Chat, &fake{t: t})
		chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"r1","model":"`+c.served+`"}}`,
				`data: {"type":"response.completed","response":{"id":"r1","model":"`+c.served+`","usage":{"input_tokens":9,"output_tokens":2}}}`))
		})
		s := New()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":"hi"}`))
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		s.Handler().ServeHTTP(rec, req)
		r := lastRoute(s)
		if r.Served != c.served || r.Swapped != c.swapped || r.Tries[0].Served != c.served || r.Tries[0].Swapped != c.swapped {
			t.Errorf("%s: route %+v", c.served, r)
		}
	}
}
