package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/gjson"
)

// openAIRefusal is OpenAI's own 400 for max_tokens on its newer models
// (GoxxKintBlade on X: "4 个模型中有 4 个没有响应；gpt-6.1-sol：400").
const openAIRefusal = `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.","type":"invalid_request_error","param":"max_tokens","code":"unsupported_parameter"}}`

// A model's test on OpenAI's own Chat Completions names its reply length
// max_completion_tokens; DeepSeek's, Zhipu's and a relay's keep
// max_tokens, which they read.
func TestOpenAIProbeAsksMaxCompletionTokens(t *testing.T) {
	for _, c := range []struct {
		base, want string
	}{
		{"https://api.openai.com/v1", "max_completion_tokens"},
		{"https://api.deepseek.com/v1", "max_tokens"},
		{"https://open.bigmodel.cn/api/paas/v4", "max_tokens"},
		{"https://relay.example/v1", "max_tokens"},
		{"https://notopenai.com/v1", "max_tokens"},
	} {
		_, body := tinyBody(Provider{ID: "x", Chat: c.base}, Chat, "gpt-6.1-sol")
		if gjson.Get(body, c.want).Int() != 16 {
			t.Errorf("%s: %s", c.base, body)
		}
		other := "max_tokens"
		if c.want == other {
			other = "max_completion_tokens"
		}
		if gjson.Get(body, other).Exists() {
			t.Errorf("%s: both named: %s", c.base, body)
		}
	}
}

// A relay that passes on OpenAI's 400 for max_tokens has the test asked
// once more with max_completion_tokens, and the model is green; a refusal
// of the value is asked once.
func TestProbeRetriesTokenFieldNamed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(b))
		mu.Unlock()
		switch {
		case gjson.GetBytes(b, "model").String() == "capped":
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(rw, `{"error":{"message":"max_tokens must be at most 8"}}`)
		case gjson.GetBytes(b, "max_tokens").Exists():
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(rw, openAIRefusal)
		default:
			_, _ = io.WriteString(rw, `{"choices":[{"message":{"content":"hi"}}]}`)
		}
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Key: "k", Models: []string{"gpt-6.1-sol", "capped"}}
	rs := p.TestModels(context.Background(), []string{"gpt-6.1-sol"})
	if !rs[0].OK {
		t.Fatalf("%+v", rs[0])
	}
	if len(got) != 2 || gjson.Get(got[1], "max_completion_tokens").Int() != 16 || gjson.Get(got[1], "max_tokens").Exists() {
		t.Fatalf("asked %q", got)
	}
	got = nil
	rs = p.TestModels(context.Background(), []string{"capped"})
	if rs[0].OK || rs[0].Status != 400 || len(got) != 1 {
		t.Fatalf("%+v, asked %q", rs[0], got)
	}
}

func TestTokenFieldWanted(t *testing.T) {
	for msg, want := range map[string]string{
		openAIRefusal: "max_completion_tokens",
		"Unsupported parameter: 'max_completion_tokens'. Please use `max_tokens` instead.": "max_tokens",
		"Use max_tokens instead of max_completion_tokens":                                  "max_tokens",
		"max_tokens must be at most 8192":                                                  "",
		"max_completion_tokens is too large: 200000":                                       "",
		"input length and `max_tokens` exceed context limit: 1 + 2 > 2":                    "",
	} {
		if got := TokenFieldWanted([]byte(msg)); got != want {
			t.Errorf("%q: %q, want %q", msg, got, want)
		}
	}
}

func TestWithTokenField(t *testing.T) {
	in := `{"model":"m", "max_tokens" : 16,"messages":[{"role":"user","content":"<b> max_tokens"}]}`
	out, ok := WithTokenField([]byte(in), CompletionTokField)
	if !ok || string(out) != strings.Replace(in, `"max_tokens" :`, `"max_completion_tokens" :`, 1) {
		t.Fatalf("%v %s", ok, out)
	}
	back, ok := WithTokenField(out, MaxTokensField)
	if !ok || string(back) != in {
		t.Fatalf("%v %s", ok, back)
	}
	if _, ok := WithTokenField([]byte(`{"messages":[{"max_tokens":1}]}`), CompletionTokField); ok {
		t.Fatal("a nested key renamed")
	}
	both, ok := WithTokenField([]byte(`{"max_tokens":7,"max_completion_tokens":5}`), CompletionTokField)
	if !ok || gjson.GetBytes(both, "max_tokens").Exists() || gjson.GetBytes(both, "max_completion_tokens").Int() != 5 {
		t.Fatalf("%v %s", ok, both)
	}
}
