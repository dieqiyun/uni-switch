package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// the vendor's reply headers, each kind of them: request ids passed on by
// default, one the user lists in another case, and ones never passed on
func setVendorHeaders(h http.Header, id string) {
	h.Set("X-Oneapi-Request-Id", "oneapi-"+id)
	h.Set("X-Request-Id", "xr-"+id)
	h.Set("X-Trace-Token", "trace-"+id)
	h.Set("X-Unlisted", "no")
	h.Set("Set-Cookie", "session=secret")
	h.Set("Access-Control-Allow-Origin", "*")
}

const (
	rhAnthropicJSON = `{"id":"msg_1","type":"message","role":"assistant","model":"sol","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	rhAnthropicSSE  = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"sol\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	rhChatJSON = `{"id":"c1","object":"chat.completion","model":"sol","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	rhChatSSE  = "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"sol\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n" +
		"data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"sol\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
)

// vendorReply answers as vendor does on its wire: a stream when asked for
// one, an error with status when it isn't 200.
func vendorReply(vendor provider.Protocol, status int, id string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := rhBody(r)
		setVendorHeaders(w.Header(), id)
		if status != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request, ` + id + `"}}`))
			return
		}
		stream := strings.Contains(body, `"stream":true`)
		switch {
		case stream:
			w.Header().Set("Content-Type", "text/event-stream")
			if vendor == provider.Chat {
				w.Write([]byte(rhChatSSE))
			} else {
				w.Write([]byte(rhAnthropicSSE))
			}
		case vendor == provider.Chat:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(rhChatJSON))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(rhAnthropicJSON))
		}
	}
}

func rhBody(r *http.Request) string {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}

func rhVendor(t *testing.T, p provider.Provider, vendor provider.Protocol, h http.Handler) {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	if vendor == provider.Chat {
		p.Chat = up.URL + "/v1"
	} else {
		p.Anthropic = up.URL
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
}

// checkPassed fails unless h carries the vendor's request ids and the
// listed header of reply id, and nothing never passed on or unlisted.
func checkPassed(t *testing.T, h http.Header, id string) {
	t.Helper()
	for k, want := range map[string]string{
		"X-Oneapi-Request-Id": "oneapi-" + id,
		"X-Request-Id":        "xr-" + id,
		"X-Trace-Token":       "trace-" + id,
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	for _, k := range []string{"X-Unlisted", "Set-Cookie"} {
		if got := h.Get(k); got != "" {
			t.Errorf("%s = %q passed on", k, got)
		}
	}
}

// #4145: the vendor's request ids, and the headers the provider lists in
// any case, reach the agent, relayed or translated, streamed or whole, and
// on the vendor's error; never a cookie or one the user didn't list.
func TestReplyHeadersPassedOn(t *testing.T) {
	for _, vendor := range []provider.Protocol{provider.Anthropic, provider.Chat} {
		for _, stream := range []bool{false, true} {
			for _, status := range []int{200, 400} {
				name := string(vendor)
				if stream {
					name += "/stream"
				}
				if status != 200 {
					name += "/error"
				}
				t.Run(name, func(t *testing.T) {
					fresh(t)
					rhVendor(t, provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"sol"},
						ReplyHeaders: []string{"x-trace-token", "set-cookie"}}, vendor, vendorReply(vendor, status, "one"))
					body := `{"model":"fake/sol","max_tokens":100,"messages":[{"role":"user","content":"hi"}]` +
						map[bool]string{true: `,"stream":true}`, false: `}`}[stream]
					rec := httptest.NewRecorder()
					New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
					if rec.Code != status {
						t.Fatalf("%d %s", rec.Code, rec.Body)
					}
					checkPassed(t, rec.Header(), "one")
					if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "*" {
						t.Errorf("the vendor's CORS header passed on")
					}
				})
			}
		}
	}
}

// A request uni-switch took to a second provider after the first refused it
// carries the second's headers alone (#4145).
func TestReplyHeadersOfTheReplyGiven(t *testing.T) {
	fresh(t)
	noRests()
	t.Cleanup(noRests)
	rhVendor(t, provider.Provider{ID: "ag", Name: "AG", Key: "k", Models: []string{"sol"}, Fallback: []string{"spare/sol"},
		ReplyHeaders: []string{"X-Trace-Token"}}, provider.Anthropic, vendorReply(provider.Anthropic, 429, "first"))
	rhVendor(t, provider.Provider{ID: "spare", Name: "Spare", Key: "k", Models: []string{"sol"}},
		provider.Chat, vendorReply(provider.Chat, 200, "second"))
	rec := httptest.NewRecorder()
	body := `{"model":"ag/sol","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	h := rec.Header()
	if h.Get("X-Request-Id") != "xr-second" || h.Get("X-Oneapi-Request-Id") != "oneapi-second" {
		t.Errorf("ids %q %q, want the second's", h.Get("X-Request-Id"), h.Get("X-Oneapi-Request-Id"))
	}
	// spare lists nothing: the first provider's listed header went with its try
	if got := h.Get("X-Trace-Token"); got != "" {
		t.Errorf("X-Trace-Token = %q, from a reply not given", got)
	}
}

// count_tokens passes the vendor's ids on too.
func TestReplyHeadersOnCountTokens(t *testing.T) {
	fresh(t)
	rhVendor(t, provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"sol"}, ReplyHeaders: []string{"X-Trace-Token"}},
		provider.Anthropic, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setVendorHeaders(w.Header(), "count")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"input_tokens":3}`))
		}))
	rec := httptest.NewRecorder()
	body := `{"model":"fake/sol","messages":[{"role":"user","content":"hi"}]}`
	New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	checkPassed(t, rec.Header(), "count")
}

// Embeddings pass the vendor's ids on, its answer's and its error's.
func TestReplyHeadersOnEmbeddings(t *testing.T) {
	for _, status := range []int{200, 400} {
		fresh(t)
		rhVendor(t, provider.Provider{ID: "lib", Name: "Lib", Key: "k", Models: []string{"embed-1"}, ReplyHeaders: []string{"X-Trace-Token"}},
			provider.Chat, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				setVendorHeaders(w.Header(), "embed")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status != 200 {
					w.Write([]byte(`{"error":{"message":"not an embedding model"}}`))
					return
				}
				w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"embed-1","usage":{"prompt_tokens":1,"total_tokens":1}}`))
			}))
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(`{"model":"lib/embed-1","input":"hi"}`)))
		if rec.Code != status {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		checkPassed(t, rec.Header(), "embed")
	}
}
