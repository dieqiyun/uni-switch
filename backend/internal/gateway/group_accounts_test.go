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

// keyedUpstream answers chat, messages and count_tokens, each reply
// naming the key it came in on, and records the keys it was asked on.
// refuse, when set, answers that key with a 429.
type keyedUpstream struct {
	mu     sync.Mutex
	keys   []string
	refuse map[string]bool
}

func (u *keyedUpstream) asked() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.keys...)
}

func (u *keyedUpstream) reset() {
	u.mu.Lock()
	u.keys = nil
	u.mu.Unlock()
}

func (u *keyedUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Header.Get("x-api-key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	u.mu.Lock()
	u.keys = append(u.keys, key)
	refused := u.refuse[key]
	u.mu.Unlock()
	if refused {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit_error"}}`)
		return
	}
	stream := strings.Contains(string(body), `"stream":true`)
	text := "from " + key
	switch {
	case strings.HasSuffix(r.URL.Path, "/count_tokens"):
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"input_tokens":5}`)
	case strings.HasSuffix(r.URL.Path, "/messages") && stream:
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+text+`"}}`,
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
	case strings.HasSuffix(r.URL.Path, "/messages"):
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"`+text+`"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	case stream:
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"`+text+`"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			`data: [DONE]`))
	default:
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"`+text+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}
}

// groupAccountsSetup: a relay "relay" with a personal key and a company
// key, speaking chat and messages, and a second provider "other" with
// one key, both serving m.
func groupAccountsSetup(t *testing.T) (relay, other *keyedUpstream) {
	t.Helper()
	fresh(t)
	t.Cleanup(noRests)
	relay, other = &keyedUpstream{}, &keyedUpstream{}
	ru, ou := httptest.NewServer(relay), httptest.NewServer(other)
	t.Cleanup(ru.Close)
	t.Cleanup(ou.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: ru.URL + "/v1", Anthropic: ru.URL, Key: "sk-mine",
		Keys: []provider.KeyAccount{{Key: "sk-work"}}, Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "other", Name: "Other", Chat: ou.URL + "/v1", Anthropic: ou.URL, Key: "sk-other", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	return relay, other
}

func askGroup(h http.Handler, secret, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	h.ServeHTTP(rec, req)
	return rec
}

// every route a request through a group can take, each one asked for
// the model given
func groupRoutes(model string) [][2]string {
	return [][2]string{
		{"/v1/chat/completions", `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/chat/completions", `{"model":"` + model + `","stream":true,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/responses", `{"model":"` + model + `","input":"hi"}`},
		{"/v1/responses", `{"model":"` + model + `","stream":true,"input":"hi"}`},
		{"/v1/messages", `{"model":"` + model + `","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/messages", `{"model":"` + model + `","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/messages/count_tokens", `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`},
	}
}
