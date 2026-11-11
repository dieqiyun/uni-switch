package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// clientOnlyRelay is a relay that serves only the agents its plan is sold
// for, telling them by their User-Agent as sub2api's codex_cli_only and
// packy's Claude Code-only plans do: 403 to anyone else, uni-switch's own test
// request among them (耍赖天都爱 on Discord).
type clientOnlyRelay struct {
	mu   sync.Mutex
	head http.Header
	body map[string]any
	path string
}

func (f *clientOnlyRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.head, f.path, f.body = r.Header.Clone(), r.URL.Path, nil
	json.Unmarshal(b, &f.body)
	f.mu.Unlock()
	ua := r.Header.Get("User-Agent")
	ok := strings.HasSuffix(r.URL.Path, "/messages") && strings.HasPrefix(ua, "claude-cli/") ||
		!strings.HasSuffix(r.URL.Path, "/messages") && strings.HasPrefix(ua, "codex_")
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"message":"This account only allows Codex official clients","type":"forbidden"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages"):
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
	case strings.HasSuffix(r.URL.Path, "/responses"):
		io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
	default:
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	}
}

func (f *clientOnlyRelay) last() (http.Header, map[string]any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, f.body, f.path
}

// A model's test can be asked as Codex or Claude Code asks, for a relay
// that serves only them: as Codex, on Responses, streamed and not stored,
// with Codex's headers; as Claude Code, only once Claude Code has come
// through uni-switch, with the headers it came with — never its session or
// the betas of one request — and never of Anthropic's own API.
func TestModelTestAsClient(t *testing.T) {
	fresh(t)
	f := &clientOnlyRelay{}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gpt-5.5", "claude-opus-4-8"},
		Chat: up.URL + "/v1", Responses: up.URL + "/v1", Anthropic: up.URL}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	test := func(client, model string) provider.Result {
		t.Helper()
		return p.TestModels(provider.TestAs(context.Background(), client), []string{model})[0]
	}

	// as uni-switch asks: turned away, though Codex's own requests are served
	if r := test("", "gpt-5.5"); r.OK || r.Status != 403 {
		t.Fatalf("plain test: %+v", r)
	}
	if got := p.TestClients(); got[provider.ClientCodex] != "" || !strings.Contains(got[provider.ClientClaudeCode], "no Claude Code request") {
		t.Errorf("clients before Claude Code came: %q", got)
	}

	r := test(provider.ClientCodex, "gpt-5.5")
	head, body, path := f.last()
	if !r.OK || r.Protocol != provider.Responses || path != "/v1/responses" {
		t.Fatalf("as Codex: %+v on %s", r, path)
	}
	if !strings.HasPrefix(head.Get("User-Agent"), "codex_cli_rs/") || head.Get("Originator") != "codex_cli_rs" || head.Get("Authorization") != "Bearer k" {
		t.Errorf("as Codex, headers: %v", head)
	}
	if body["stream"] != true || body["store"] != false {
		t.Errorf("as Codex, body: %v", body)
	}

	// Claude Code's headers are never made up before it has come through
	r = test(provider.ClientClaudeCode, "claude-opus-4-8")
	if r.OK || !strings.Contains(r.Error, "no Claude Code request") {
		t.Fatalf("as Claude Code before it came: %+v", r)
	}
	claude := http.Header{
		"User-Agent":               {"claude-cli/2.1.90 (external, cli)"},
		"X-App":                    {"cli"},
		"Anthropic-Version":        {"2023-06-01"},
		"Anthropic-Beta":           {"claude-code-20250219,context-1m-2025-08-07"},
		"X-Stainless-Lang":         {"js"},
		"X-Claude-Code-Session-Id": {"ses-1"},
		"Authorization":            {"Bearer " + Token},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"relay/claude-opus-4-8","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
	for k, v := range claude {
		req.Header[k] = v
	}
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("Claude Code through uni-switch: %d %s", rec.Code, rec.Body.String())
	}

	r = test(provider.ClientClaudeCode, "claude-opus-4-8")
	head, body, path = f.last()
	if !r.OK || r.Protocol != provider.Anthropic || path != "/v1/messages" {
		t.Fatalf("as Claude Code: %+v on %s", r, path)
	}
	for _, k := range []string{"User-Agent", "X-App", "X-Stainless-Lang"} {
		if head.Get(k) != claude.Get(k) {
			t.Errorf("as Claude Code, %s: %q", k, head.Get(k))
		}
	}
	for _, k := range []string{"X-Claude-Code-Session-Id", "Anthropic-Beta"} {
		if head.Get(k) != "" {
			t.Errorf("as Claude Code, %s went: %q", k, head.Get(k))
		}
	}
	if head.Get("X-Api-Key") != "k" || body["stream"] != true {
		t.Errorf("as Claude Code: key %q, body %v", head.Get("X-Api-Key"), body)
	}
	// the whole provider's test, as the CLI's without models
	if rs := p.Test(provider.TestAs(context.Background(), provider.ClientClaudeCode)); len(rs) != 1 || !rs[0].OK || rs[0].Protocol != provider.Anthropic {
		t.Errorf("provider test as Claude Code: %+v", rs)
	}

	// never of Anthropic's own API
	a := *p
	a.Anthropic, a.Chat, a.Responses = "https://api.anthropic.com", "", ""
	if why := a.TestsAs(provider.ClientClaudeCode); !strings.Contains(why, "only of a relay") {
		t.Errorf("Anthropic's API as Claude Code: %q", why)
	}
	if _, ok := a.TestClients()[provider.ClientClaudeCode]; ok {
		t.Errorf("Anthropic's API offers Claude Code: %v", a.TestClients())
	}
}

// Claude Code through uni-switch on a model a relay doesn't serve on Messages
// — a Chat-only provider here, a subscription or a translated model alike —
// still lets a relay's models be tested as Claude Code: what a test asks
// with is Claude Code's headers, whatever served its request (neiko on
// Discord: connected, and "Test as Claude Code" stayed grey).
func TestClaudeCodeSeenOnAnyRoute(t *testing.T) {
	fresh(t)
	f := &clientOnlyRelay{}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(chat.Close)
	for _, p := range []provider.Provider{
		{ID: "relay", Name: "Relay", Key: "k", Models: []string{"claude-opus-4-8"}, Anthropic: up.URL},
		{ID: "glm", Name: "GLM", Key: "g", Models: []string{"glm-4.6"}, Chat: chat.URL + "/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if why := p.TestClients()[provider.ClientClaudeCode]; !strings.Contains(why, "no Claude Code request") {
		t.Fatalf("before Claude Code came: %q", why)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"glm/glm-4.6","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("User-Agent", "claude-cli/2.1.200 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Authorization", "Bearer "+Token)
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("Claude Code on a Chat provider: %d %s", rec.Code, rec.Body.String())
	}

	if why := p.TestClients()[provider.ClientClaudeCode]; why != "" {
		t.Fatalf("after Claude Code came on a Chat provider: %q", why)
	}
	r := p.TestModels(provider.TestAs(context.Background(), provider.ClientClaudeCode), []string{"claude-opus-4-8"})[0]
	head, _, _ := f.last()
	if !r.OK || head.Get("User-Agent") != "claude-cli/2.1.200 (external, cli)" || head.Get("X-App") != "cli" {
		t.Errorf("as Claude Code: %+v, headers %v", r, head)
	}
}
