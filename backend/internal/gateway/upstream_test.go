package gateway

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// orAnswers is an aggregator that answers as OpenRouter does (seen
// 2026-10-05): a Chat body and every chunk of its stream say the
// provider behind it in a top-level "provider", an Anthropic body too, and
// an Anthropic stream in message_start's message.
func orAnswers(proto provider.Protocol, upstream string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		stream := strings.Contains(string(b), `"stream":true`)
		P := `"provider":"` + upstream + `"`
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			if proto == provider.Chat {
				io.WriteString(w, `{"id":"gen-1","object":"chat.completion","model":"deepseek/deepseek-chat",`+P+`,"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
			} else {
				io.WriteString(w, `{"id":"gen-1","type":"message","role":"assistant","model":"deepseek/deepseek-chat","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2},`+P+`}`)
			}
			return
		}
		var events []string
		if proto == provider.Chat {
			events = []string{
				`data: {"id":"gen-1","model":"deepseek/deepseek-chat",` + P + `,"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
				`data: {"id":"gen-1","model":"deepseek/deepseek-chat",` + P + `,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
				`data: [DONE]`,
			}
		} else {
			events = []string{
				"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"gen-1","type":"message","role":"assistant","model":"deepseek/deepseek-chat","content":[],"usage":{"input_tokens":5,"output_tokens":0},` + P + `}}`,
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

// The provider an aggregator says answered behind it (leslie_luo on
// Discord: "指出实际供应商" for ClinePass, OpenRouter) is kept on the
// request's route, its try, the request log and the usage ledger, relayed
// or translated, streamed or not.
func TestUpstreamRecorded(t *testing.T) {
	bodies := map[string]string{
		"/v1/messages":         `{"model":"fake/sol","max_tokens":100,"messages":[{"role":"user","content":"hi"}]`,
		"/v1/chat/completions": `{"model":"fake/sol","messages":[{"role":"user","content":"hi"}]`,
		"/v1/responses":        `{"model":"fake/sol","input":"hi"`,
	}
	for _, pa := range []struct {
		name  string
		proto provider.Protocol // the aggregator's
		agent string
	}{
		{"chat", provider.Chat, "/v1/chat/completions"},
		{"anthropic", provider.Anthropic, "/v1/messages"},
		{"anthropic from chat", provider.Chat, "/v1/messages"},
		{"responses from chat", provider.Chat, "/v1/responses"},
		{"chat from anthropic", provider.Anthropic, "/v1/chat/completions"},
		{"responses from anthropic", provider.Anthropic, "/v1/responses"},
	} {
		for _, stream := range []bool{true, false} {
			name := pa.name
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				fresh(t)
				up := httptest.NewServer(orAnswers(pa.proto, "DeepInfra"))
				t.Cleanup(up.Close)
				p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"sol"}}
				if pa.proto == provider.Chat {
					p.Chat = up.URL + "/v1"
				} else {
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
				if r.Upstream != "DeepInfra" || len(r.Tries) != 1 || r.Tries[0].Upstream != "DeepInfra" {
					t.Errorf("route upstream %q, tries %+v", r.Upstream, r.Tries)
				}
				if calls := s.Recent(); len(calls) == 0 || calls[0].Usage.Upstream != "DeepInfra" {
					t.Errorf("request log: %+v", calls)
				}
				if u := usage.Load(time.Time{}); len(u) != 1 || u[0].Upstream != "DeepInfra" {
					t.Errorf("usage %+v", u)
				}
			})
		}
	}
}

// Only a name is an upstream: a reply without one, or one echoing a
// request's provider options (an object), says none.
func TestUpstreamOf(t *testing.T) {
	for in, want := range map[string]string{
		`{"id":"c1","provider":"Novita","choices":[]}`:                  "Novita",
		`{"type":"message_start","message":{"provider":"StreamLake"}}`:  "StreamLake",
		`{"id":"c1","choices":[]}`:                                      "",
		`{"provider":{"order":["deepseek"]},"choices":[]}`:              "",
		`{"provider":"  "}`:                                             "",
		`{"choices":[{"delta":{"content":"the \"provider\" field"}}]}`:  "",
		`not json "provider"`:                                           "",
		`{"type":"response.created","response":{"provider":"Baseten"}}`: "Baseten",
	} {
		if got := upstreamOf([]byte(in)); got != want {
			t.Errorf("upstreamOf(%s) = %q; want %q", in, got, want)
		}
	}
}

// clinePassStream is a real ClinePass stream (api.cline.bot, DeepSeek V4.1
// Flash, 2026-10-02), as captured in wkeking/clinepass-channel-monitor's
// testdata/plain_success.log (MIT): its last chunk names the provider that
// answered only in choices[].delta.provider_metadata.gateway.routing.
func clinePassStream(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/clinepass_stream.sse")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		if l := sc.Text(); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// ClinePass says the provider behind it as finalProvider in a choice's
// provider_metadata (#2847, smart-lty): under delta in a stream, under
// message in one body. Only there: the model's own text naming it, or the
// list of providers tried, says none.
func TestUpstreamOfClinePass(t *testing.T) {
	var last string
	for _, l := range clinePassStream(t) {
		if d := strings.TrimPrefix(l, "data: "); strings.Contains(d, "finalProvider") {
			last = d
		}
	}
	if got := upstreamOf([]byte(last)); got != "deepseek" {
		t.Errorf("real ClinePass chunk: upstreamOf = %q; want deepseek", got)
	}
	whole := strings.Replace(last, `"delta":{"provider_metadata"`, `"message":{"role":"assistant","content":"hi","provider_metadata"`, 1)
	if got := upstreamOf([]byte(whole)); got != "deepseek" {
		t.Errorf("one body: upstreamOf = %q; want deepseek", got)
	}
	for in, want := range map[string]string{
		`{"choices":[{"delta":{"provider_metadata":{"gateway":{"routing":{"finalProvider":"novita"}}}}}]}`:                                         "novita",
		`{"choices":[{"delta":{"content":"provider_metadata gateway routing \"finalProvider\":\"deepseek\""}}]}`:                                   "",
		`{"choices":[{"delta":{"provider_metadata":{"gateway":{"routing":{"modelAttempts":[{"providerAttempts":[{"provider":"deepseek"}]}]}}}}}]}`: "",
		`{"choices":[{"delta":{"provider_metadata":{"gateway":{"routing":{"finalProvider":{"id":"x"}}}}}}]}`:                                       "",
		`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"finalProvider\":\"x\"}"}}]}}]}`:                                          "",
		`{"provider":"Novita","choices":[{"delta":{"provider_metadata":{"gateway":{"routing":{"finalProvider":"deepseek"}}}}}]}`:                   "Novita",
	} {
		if got := upstreamOf([]byte(in)); got != want {
			t.Errorf("upstreamOf(%s) = %q; want %q", in, got, want)
		}
	}
}

// The real ClinePass stream's provider reaches the route, its try, the
// request log and the usage ledger, relayed to a Chat client or translated
// to a Messages one (#2847).
func TestClinePassUpstreamRecorded(t *testing.T) {
	stream := clinePassStream(t)
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			fresh(t)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, l := range stream {
					io.WriteString(w, l+"\n\n")
				}
			}))
			t.Cleanup(up.Close)
			p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: up.URL + "/v1", Models: []string{"sol"}}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			body := `{"model":"fake/sol","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
			s := New()
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
			if rec.Code != 200 {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			r := lastRoute(s)
			if r.Upstream != "deepseek" || len(r.Tries) != 1 || r.Tries[0].Upstream != "deepseek" {
				t.Errorf("route upstream %q, tries %+v", r.Upstream, r.Tries)
			}
			if calls := s.Recent(); len(calls) == 0 || calls[0].Usage.Upstream != "deepseek" {
				t.Errorf("request log: %+v", calls)
			}
			if u := usage.Load(time.Time{}); len(u) != 1 || u[0].Upstream != "deepseek" {
				t.Errorf("usage %+v", u)
			}
		})
	}
}
