package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// sentOutputEffort is the output_config.effort a Messages body asks for.
func sentOutputEffort(t *testing.T, body []byte) string {
	t.Helper()
	var q struct {
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return q.OutputConfig.Effort
}

// DeepSeek's Anthropic endpoint ignores budget_tokens and takes the
// effort in output_config.effort alone (api-docs.deepseek.com,
// guides/anthropic_api and guides/thinking_mode): a client that asks by
// budget, as DSH's uni-switch-anthropic route does through pi-ai, has its
// level said there too, fitted to DeepSeek's low, high and max (#3298).
// The same model behind another host, and another vendor's model, go as
// they were sent.
func TestDeepSeekAnthropicEffortInOutputConfig(t *testing.T) {
	f := &fake{t: t, ctype: "application/json", reply: anthropicWholeOK}
	setup(t, provider.Anthropic, f)
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	for _, p := range []provider.Provider{
		{ID: "deepseek", Name: "DeepSeek", Key: "k", Models: []string{"deepseek-flash", "deepseek-v4-pro"}, Anthropic: "http://api.deepseek.com/anthropic"},
		{ID: "relay", Name: "Relay", Key: "k", Models: []string{"deepseek-flash"}, Anthropic: "http://relay.example.com/anthropic"},
		{ID: "kimi", Name: "Kimi", Key: "k", Models: []string{"kimi-k2.5"}, Anthropic: "http://api.moonshot.ai/anthropic"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	const body = `{"model":"%s","max_tokens":64000,"stream":false,"thinking":{"type":"enabled","budget_tokens":%s},"messages":[{"role":"user","content":"hi"}]}`
	for _, c := range []struct{ model, budget, want string }{
		{"deepseek/deepseek-flash", "4096", "low"},
		{"deepseek/deepseek-flash", "10000", "high"},
		{"deepseek/deepseek-v4-pro", "32000", "max"},
		// not DeepSeek's endpoint: its docs say nothing of a relay's
		{"relay/deepseek-flash", "32000", ""},
		// another vendor's model on its own Anthropic endpoint
		{"kimi/kimi-k2.5", "32000", ""},
	} {
		f.got = nil
		req := strings.Replace(strings.Replace(body, "%s", c.model, 1), "%s", c.budget, 1)
		code, reply := postTo(t, s, "/v1/messages", req)
		if code != 200 {
			t.Fatalf("%s: %d %s", c.model, code, reply)
		}
		if got := sentOutputEffort(t, f.got); got != c.want {
			t.Errorf("%s budget %s: output_config.effort %q, want %q: %s", c.model, c.budget, got, c.want, f.got)
		}
		if !strings.Contains(string(f.got), `"budget_tokens":`+c.budget) {
			t.Errorf("%s: the budget was not sent as it came: %s", c.model, f.got)
		}
	}
}

// Translated onto DeepSeek's Anthropic endpoint (a Chat client), the
// client's reasoning_effort goes in output_config beside the budget; the
// same model on another host, and another vendor's model on DeepSeek's
// builder path, get the budget alone as before.
func TestTranslatedDeepSeekAnthropicEffort(t *testing.T) {
	r := &Request{Thinking: true, Effort: "max", Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}}}}
	for _, c := range []struct{ host, model, want string }{
		{"api.deepseek.com", "deepseek-flash", "max"},
		{"api.deepseek.com", "deepseek-v4-pro", "max"},
		{"relay.example.com", "deepseek-flash", ""},
		{"api.moonshot.ai", "kimi-k2.5", ""},
		{"", "kimi-k2.5", ""},
	} {
		out, err := build(provider.Anthropic, r, c.model, c.host, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := sentOutputEffort(t, out); got != c.want {
			t.Errorf("%s at %q: output_config.effort %q, want %q: %s", c.model, c.host, got, c.want, out)
		}
		if !strings.Contains(string(out), `"budget_tokens":`) {
			t.Errorf("%s at %q: no budget: %s", c.model, c.host, out)
		}
	}
}
