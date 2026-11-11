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

// openAIRefusal is OpenAI's own 400 for max_tokens on its newer models
// (GoxxKintBlade on X, gpt-6.1-sol).
const openAIRefusal = `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.","type":"invalid_request_error","param":"max_tokens","code":"unsupported_parameter"}}`

// A Chat Completions request to OpenAI's own API names its reply length
// max_completion_tokens; DeepSeek's, Zhipu's and a relay's go as they came.
func TestOpenAIChatGetsMaxCompletionTokens(t *testing.T) {
	fresh(t)
	s := New()
	body := []byte(`{"model":"m","max_tokens":4096,"messages":[{"role":"user","content":"what is \"max_tokens\"?"}]}`)
	for _, c := range []struct {
		base string
		want string
	}{
		{"https://api.openai.com/v1", "max_completion_tokens"},
		{"https://api.deepseek.com/v1", "max_tokens"},
		{"https://open.bigmodel.cn/api/paas/v4", "max_tokens"},
		{"https://relay.example/v1", "max_tokens"},
		{"https://notopenai.com/v1", "max_tokens"},
	} {
		p := provider.Provider{ID: "x", Chat: c.base}
		got := s.chatTokenField(p, provider.Chat, body)
		if n := gjson.GetBytes(got, c.want).Int(); n != 4096 {
			t.Errorf("%s: sent %s", c.base, got)
		}
		if c.want == "max_tokens" && string(got) != string(body) {
			t.Errorf("%s: body changed: %s", c.base, got)
		}
		if !strings.Contains(string(got), `what is \"max_tokens\"?`) {
			t.Errorf("%s: message text changed: %s", c.base, got)
		}
	}
	// another API's body is never touched
	p := provider.Provider{ID: "x", Chat: "https://api.openai.com/v1"}
	if got := s.chatTokenField(p, provider.Anthropic, body); string(got) != string(body) {
		t.Errorf("anthropic body changed: %s", got)
	}
}

// A relay in front of OpenAI's newer models passes on its 400 for
// max_tokens: the request is asked once more with max_completion_tokens,
// and the next one goes with it at once. A vendor that takes max_tokens
// gets the body as it came, in one request.
func TestChatTokenFieldRetriedWhenNamed(t *testing.T) {
	fresh(t)
	ok := sse(
		`data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		`data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	)
	var bodies [][]byte
	f := &fake{reply: ok, refuse: func(b []byte) (int, string) {
		bodies = append(bodies, b)
		if gjson.GetBytes(b, "max_tokens").Exists() {
			return http.StatusBadRequest, openAIRefusal
		}
		return 0, ""
	}}
	up := httptest.NewServer(f)
	defer up.Close()
	var otherGot [][]byte
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		otherGot = append(otherGot, b)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ok)
	}))
	defer other.Close()
	for _, p := range []provider.Provider{
		{ID: "relay", Name: "Relay", Key: "k", Chat: up.URL + "/v1", Models: []string{"gpt-6.1-sol"}},
		{ID: "zhipu-like", Name: "Other", Key: "k", Chat: other.URL + "/v1", Models: []string{"glm-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	h := New().Handler()
	ask := func(path, body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s: HTTP %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	chat := `{"model":"relay/gpt-6.1-sol","stream":true,"max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`
	ask("/v1/chat/completions", chat)
	if len(bodies) != 2 {
		t.Fatalf("relay asked %d times: %q", len(bodies), bodies)
	}
	last := bodies[1]
	if gjson.GetBytes(last, "max_tokens").Exists() || gjson.GetBytes(last, "max_completion_tokens").Int() != 4096 {
		t.Fatalf("retry sent %s", last)
	}
	// remembered: the next request, translated from Claude's API, goes
	// with max_completion_tokens at once
	ask("/v1/messages", `{"model":"relay/gpt-6.1-sol","stream":true,"max_tokens":300,"messages":[{"role":"user","content":"hi"}]}`)
	if len(bodies) != 3 {
		t.Fatalf("relay asked %d times: %q", len(bodies), bodies)
	}
	if gjson.GetBytes(bodies[2], "max_tokens").Exists() || !gjson.GetBytes(bodies[2], "max_completion_tokens").Exists() {
		t.Fatalf("second request sent %s", bodies[2])
	}
	// another vendor, which reads max_tokens, gets it as sent
	ask("/v1/chat/completions", `{"model":"zhipu-like/glm-5","stream":true,"max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`)
	if len(otherGot) != 1 {
		t.Fatalf("other asked %d times", len(otherGot))
	}
	var sent map[string]any
	if err := json.Unmarshal(otherGot[0], &sent); err != nil {
		t.Fatal(err)
	}
	if sent["max_tokens"] != float64(4096) || sent["max_completion_tokens"] != nil {
		t.Fatalf("other got %s", otherGot[0])
	}
}
