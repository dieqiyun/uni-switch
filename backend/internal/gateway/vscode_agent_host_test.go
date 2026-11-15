package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// VS Code's Agents window runs `codex app-server` (main at 8fb6054,
// 11-01: @openai/codex 0.163.0) with clientInfo {name: "vscode_agent_host"}, in
// src/vs/platform/agentHost/node/codex/codexAgent.ts. Codex sends the name
// as its originator and at the head of its User-Agent, as it does Claude
// Code's plugin's (TestCodexPluginForClaudeCodeIsCodex). The headers are a
// Codex turn's under that name.
func vscodeAgentHostHeader() http.Header {
	return http.Header{
		"User-Agent":            {"vscode_agent_host/0.163.0 (Mac OS 26.2.0; arm64) unknown (vscode_agent_host; 0.1.0)"},
		"Originator":            {"vscode_agent_host"},
		"X-Codex-Window-Id":     {"019a0f7e-0000-7000-8000-000000000001:0"},
		"X-Codex-Turn-Metadata": {`{"turn_id":"019a0f7e-0000-7000-8000-000000000002","sandbox":"none"}`},
		"Session-Id":            {"019a0f7e-0000-7000-8000-000000000001"},
	}
}

// Its turns are Codex's: a Codex rule holds for them. They are not Codex's
// own name, so they never stand for Codex in a test asked as Codex.
func TestVSCodeAgentHostIsCodex(t *testing.T) {
	h := vscodeAgentHostHeader()
	if !fromCodex(h) {
		t.Fatal("VS Code's agent host's Codex turn isn't Codex's")
	}
	if !fromCodex(http.Header{"Originator": {"vscode_agent_host"}}) {
		t.Fatal("by its originator alone, it isn't Codex's")
	}
	if codexNamed(h) {
		t.Fatal("the agent host's name counted as Codex's own")
	}
	for _, other := range []string{"vscode_codex/0.163.0", "vscode_claude_code/2.1.0", "GitHubCopilotChat/0.69.0"} {
		if fromCodex(http.Header{"User-Agent": {other}}) {
			t.Errorf("%s counted as Codex", other)
		}
	}
}

// One of Codex's own models asked from the Agents window on Codex's
// uni-switch provider, which no account in uni-switch serves, goes on Codex's own
// sign-in as the Codex CLI's does (TestCodexOwnModelOnUniSwitchThreadGoesOnSignIn),
// not 404 "uni-switch knows no model" (justruman on Discord: 7 of 23 calls
// failed).
func TestVSCodeAgentHostOwnModelGoesOnSignIn(t *testing.T) {
	_, models := codexOwnOffUniSwitchSetup(t, false)
	req := codexOnUniSwitchRequest("127.0.0.1:51000")
	for k, v := range vscodeAgentHostHeader() {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if len(*models) != 1 || (*models)[0] != "gpt-6.1-sol" {
		t.Fatalf("backend asked for %v", *models)
	}
}

// Its headers go on to a keyed relay as Codex's do, but a test asked as
// Codex still asks as Codex itself: the agent host's name is no Codex
// official client a Codex-only relay lets in.
func TestVSCodeAgentHostNotCodexTestShape(t *testing.T) {
	fresh(t)
	f := &clientOnlyRelay{}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gpt-5.5"},
		Chat: up.URL + "/v1", Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"relay/gpt-5.5","stream":true,"input":"hi"}`))
	req.Header = vscodeAgentHostHeader()
	req.Header.Set("Authorization", "Bearer "+Token)
	New().Handler().ServeHTTP(httptest.NewRecorder(), req)
	head, _, _ := f.last()
	if head.Get("Originator") != "vscode_agent_host" {
		t.Fatalf("the relay wasn't sent the agent host's turn as it sent it: %v", head)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	r := p.TestModels(provider.TestAs(context.Background(), provider.ClientCodex), []string{"gpt-5.5"})[0]
	head, _, _ = f.last()
	if !r.OK || !strings.HasPrefix(head.Get("User-Agent"), "codex_cli_rs/") {
		t.Fatalf("as Codex after the agent host came: %+v, User-Agent %q", r, head.Get("User-Agent"))
	}
}
