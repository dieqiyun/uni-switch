package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// claudeCodeTurn is Claude Code 2.1.114's request as #1600 (justrun1983)
// shows it: the billing line first, a block of its own with no
// cache_control, its cch changing every turn, and the prompt after it
// marked for the cache.
func claudeCodeTurn(model, cch string) string {
	return `{"model":"` + model + `","max_tokens":32000,"stream":true,` +
		`"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.114.aff; cc_entrypoint=sdk-cli; cch=` + cch + `;"},` +
		`{"type":"text","text":"You are a Claude agent, built on Anthropic's Claude Agent SDK.","cache_control":{"type":"ephemeral"}},` +
		`{"type":"text","text":"\nYou are an interactive agent <rules> & more","cache_control":{"type":"ephemeral"}}],` +
		`"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"metadata":{"user_id":"u"}}`
}

// The same turn with the billing line left out, every other byte as sent.
func claudeCodeTurnWithoutLine(model string) string {
	return strings.Replace(claudeCodeTurn(model, "x"), `{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.114.aff; cc_entrypoint=sdk-cli; cch=x;"},`, "", 1)
}

func TestWithoutBillingLine(t *testing.T) {
	for _, cch := range []string{"9ebf3", "1778e", "0218c"} {
		if got, want := string(withoutBillingLine([]byte(claudeCodeTurn("m", cch)))), claudeCodeTurnWithoutLine("m"); got != want {
			t.Fatalf("cch %s:\n got %s\nwant %s", cch, got, want)
		}
	}
	for _, c := range []struct{ in, want string }{
		// a system string, the line ahead of the prompt
		{`{"system":"x-anthropic-billing-header: cc_version=2.1.114.aff; cch=0218c;You are <it>.","x":1}`, `{"system":"You are <it>.","x":1}`},
		// the line and the prompt in one first block
		{`{"system":[{"type":"text","text":"x-anthropic-billing-header: cch=0218c;\nYou are it.","cache_control":{"type":"ephemeral"}}]}`,
			`{"system":[{"type":"text","text":"You are it.","cache_control":{"type":"ephemeral"}}]}`},
		// a system that is the line alone: none at all
		{`{"model":"m","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.114.aff; cch=0218c;"}]}`, `{"model":"m"}`},
		// a prompt that only mentions the line, or a message that has it,
		// goes as it was sent
		{`{"system":[{"type":"text","text":"Rules mention x-anthropic-billing-header: here"}]}`, `{"system":[{"type":"text","text":"Rules mention x-anthropic-billing-header: here"}]}`},
		{`{"messages":[{"role":"user","content":"x-anthropic-billing-header: cch=1;"}]}`, `{"messages":[{"role":"user","content":"x-anthropic-billing-header: cch=1;"}]}`},
		{`{"system":[{"type":"text","text":"a"},{"type":"text","text":"b x-anthropic-billing-header: cch=1;"}]}`, `{"system":[{"type":"text","text":"a"},{"type":"text","text":"b x-anthropic-billing-header: cch=1;"}]}`},
	} {
		if got := string(withoutBillingLine([]byte(c.in))); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

// billingUpstream is a provider at an upstream that answers each API with a
// short reply and keeps the bodies it was sent.
type billingUpstream struct {
	mu     sync.Mutex
	bodies []string
}

func (u *billingUpstream) last(t *testing.T) string {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.bodies) == 0 {
		t.Fatal("nothing sent upstream")
	}
	return u.bodies[len(u.bodies)-1]
}

func newBillingUpstream(t *testing.T) (*billingUpstream, string) {
	t.Helper()
	u := &billingUpstream{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.bodies = append(u.bodies, string(b))
		u.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			io.WriteString(w, chatSSE("ok", "", ""))
		case strings.HasSuffix(r.URL.Path, "/responses"):
			io.WriteString(w, sse(
				`data: {"type":"response.output_text.delta","delta":"ok"}`,
				`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`))
		case strings.Contains(r.URL.Path, ":streamGenerateContent"):
			io.WriteString(w, sse(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":1}}`))
		default:
			io.WriteString(w, sse(
				`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":7}}}`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
				`data: {"type":"content_block_stop","index":0}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				`data: {"type":"message_stop"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return u, srv.URL
}

func claudeCodeSends(t *testing.T, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages?beta=true", strings.NewReader(body))
	req.Header.Set("User-Agent", "claude-cli/2.1.114 (external, sdk-cli)")
	req.Header.Set("Authorization", "Bearer "+Token)
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// Claude Code's billing line changes every turn (cch), and a vendor's model
// read it as the first words of the prompt: DeepSeek's prefix cache missed
// from byte 0 on every turn, 4% of a day's input read from it (#1600,
// justrun1983). On every API uni-switch asks a vendor's model on, and for
// another API's Claude, two turns that differ only in cch now send the
// same prompt, without the line, the rest of it as Claude Code sent it.
func TestBillingLineLeftOutForVendors(t *testing.T) {
	for _, c := range []struct {
		name  string
		api   func(p *provider.Provider, url string)
		model string
	}{
		{"messages", func(p *provider.Provider, url string) { p.Anthropic = url }, "deepseek-v4.1-flash"},
		{"chat", func(p *provider.Provider, url string) { p.Chat = url + "/v1" }, "deepseek-v4.1-flash"},
		{"chat claude", func(p *provider.Provider, url string) { p.Chat = url + "/v1" }, "claude-opus-5-5"},
		{"responses", func(p *provider.Provider, url string) { p.Responses = url + "/v1" }, "gpt-5.5"},
		{"gemini", func(p *provider.Provider, url string) { p.Gemini = url + "/v1beta" }, "gemini-3.5-pro"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			u, url := newBillingUpstream(t)
			p := provider.Provider{ID: "vendor", Name: "Vendor", Key: "k", Models: []string{c.model}}
			c.api(&p, url)
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			var sent []string
			for _, cch := range []string{"aaaaa", "bbbbb"} {
				claudeCodeSends(t, claudeCodeTurn("vendor/"+c.model, cch))
				sent = append(sent, u.last(t))
			}
			for _, b := range sent {
				if strings.Contains(b, "x-anthropic-billing-header") || strings.Contains(b, "cch=") {
					t.Fatalf("billing line sent: %s", b)
				}
				if !strings.Contains(b, "You are a Claude agent") || !strings.Contains(b, "You are an interactive agent") {
					t.Fatalf("prompt lost: %s", b)
				}
			}
			if sent[0] != sent[1] {
				t.Fatalf("turns differ:\n%s\n%s", sent[0], sent[1])
			}
			if c.name == "messages" {
				// the rest of Claude Code's system as it was sent, its
				// cache marks with it
				sys := gjson.Get(sent[0], "system")
				if want := gjson.Get(claudeCodeTurnWithoutLine("x"), "system").Raw; sys.Raw != want {
					t.Fatalf("system %s, want %s", sys.Raw, want)
				}
			}
		})
	}
}

// A Claude model asked on Anthropic's Messages API (Anthropic's own, a
// relay's, Bedrock's) is sent Claude Code's request as it was, billing line
// and all: a relay in front of Claude Code's own sign-in may need it.
func TestBillingLineKeptForClaude(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	u, url := newBillingUpstream(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: url, Models: []string{"claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	for _, cch := range []string{"aaaaa", "bbbbb"} {
		claudeCodeSends(t, claudeCodeTurn("relay/claude-opus-5-5", cch))
		if got, want := u.last(t), claudeCodeTurn("claude-opus-5-5", cch); got != want {
			t.Fatalf("claude body changed:\n got %s\nwant %s", got, want)
		}
	}
}
