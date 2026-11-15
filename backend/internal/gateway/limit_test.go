package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestTokenFloor(t *testing.T) {
	for msg, want := range map[string]int{
		`{"error":{"message":"max_tokens must be greater than 2"}}`:                      3,
		`max_completion_tokens must be at least 16`:                                      16,
		`Invalid 'max_output_tokens': integer below minimum value. Expected >= 16`:       16,
		`Expected a value \u003e= 16, but got 1 instead (max_output_tokens)`:             0,
		`"max_output_tokens': integer below minimum value. Expected a value \u003e= 16"`: 16,
		`max_tokens must be \u003E 2`:                                                    3,
		`max_tokens must be > 2`:                                                         3,
		`max_tokens is too large: 999999`:                                                0,
		`messages: at least 1 message is required`:                                       0,
	} {
		if got := tokenFloor([]byte(msg)); got != want {
			t.Errorf("%s: %d, want %d", msg, got, want)
		}
	}
}

// A provider that turns away a request for a reply of a token or two
// (#64: "max_tokens must be greater than 2", to an app checking the model
// is up) is asked again for the least it gives, and the app gets a reply.
func TestTooFewTokensAskedAgain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var asked []int
	stubborn := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			MaxTokens int `json:"max_tokens"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &q)
		asked = append(asked, q.MaxTokens)
		w.Header().Set("Content-Type", "application/json")
		if stubborn || q.MaxTokens > 0 && q.MaxTokens <= 2 {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"max_tokens must be greater than 2","type":"invalid_request_error"}}`)
			return
		}
		io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "bai", Name: "Bai", Key: "k", Models: []string{"m1"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	code, body := post(t, "/v1/chat/completions", `{"model":"bai/m1","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, `"ok"`) {
		t.Fatalf("status %d: %s", code, body)
	}
	if len(asked) != 2 || asked[0] != 1 || asked[1] != 3 {
		t.Fatalf("asked for %v tokens", asked)
	}

	// once is enough: a provider that still says no is the app's to see
	asked, stubborn = nil, true
	code, _ = post(t, "/v1/chat/completions", `{"model":"bai/m1","max_tokens":2,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 400 || len(asked) != 2 {
		t.Fatalf("%d after %v", code, asked)
	}
	// and a request that didn't ask for a length isn't sent again
	asked = nil
	code, _ = post(t, "/v1/chat/completions", `{"model":"bai/m1","messages":[{"role":"user","content":"hi"}]}`)
	if code != 400 || len(asked) != 1 {
		t.Fatalf("%d after %v", code, asked)
	}
	stubborn = false
}

// A floor said with >= or > (Command Code's "Expected a value >= 16") is
// read as well, whichever way the error reaches the classifier: an
// Anthropic client's error is uni-switch's own re-encoding of the vendor's,
// and a vendor may send the sign escaped itself (#260).
func TestTooFewTokensSaidWithSigns(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var said string
	var asked []int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			MaxTokens           int  `json:"max_tokens"`
			MaxCompletionTokens int  `json:"max_completion_tokens"`
			Stream              bool `json:"stream"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &q)
		n := max(q.MaxTokens, q.MaxCompletionTokens)
		asked = append(asked, n)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			w.WriteHeader(400)
			io.WriteString(w, said)
			return
		}
		if q.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(
				`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
				`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`,
				`data: [DONE]`))
			return
		}
		io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":3}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "cc", Name: "Command Code", Key: "k", Models: []string{"m1"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		said string
		want int
	}{
		{`{"error":{"message":"Invalid 'max_output_tokens': integer below minimum value. Expected a value >= 16, but got 1 instead.","type":"invalid_request_error"}}`, 16},
		{`{"error":{"message":"max_tokens must be > 2","type":"invalid_request_error"}}`, 3},
		{`{"error":{"message":"max_tokens must be \u003e 2","type":"invalid_request_error"}}`, 3},
	} {
		for _, q := range []struct{ path, body string }{
			{"/v1/messages", `{"model":"cc/m1","max_tokens":1,"messages":[{"role":"user","content":"."}]}`},
			{"/v1/chat/completions", `{"model":"cc/m1","max_tokens":1,"messages":[{"role":"user","content":"."}]}`},
		} {
			said, asked = c.said, nil
			code, body := post(t, q.path, q.body)
			if code != 200 || !strings.Contains(body, `"ok"`) {
				t.Errorf("%s, %s: status %d: %s", q.path, c.said, code, body)
			}
			if len(asked) != 2 || asked[0] != 1 || asked[1] != c.want {
				t.Errorf("%s, %s: asked for %v tokens", q.path, c.said, asked)
			}
		}
	}
}
