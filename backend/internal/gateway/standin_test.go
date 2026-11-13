package gateway

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// a model the agent names that uni-switch doesn't serve goes to the one the
// agent is set to use for it; one uni-switch serves is sent as asked
func TestStandIn(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"c1","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`}
	setup(t, provider.Chat, f)
	var asked []string
	StandIn = func(agent, model string) string { asked = append(asked, model); return "fake/m1" }
	t.Cleanup(func() { StandIn = nil })

	code, body := post(t, "/v1/chat/completions", `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || modelOf(f.got) != "m1" || !strings.Contains(body, "OK") {
		t.Fatalf("stood in: %d %s, upstream %s", code, body, f.got)
	}
	for _, m := range []string{"m1", "fake/m1", "fake/other"} {
		asked = nil
		if code, body := post(t, "/v1/chat/completions", `{"model":"`+m+`","messages":[{"role":"user","content":"hi"}]}`); code != 200 || len(asked) != 0 {
			t.Fatalf("%s: %d %s, stand-in asked for %v", m, code, body, asked)
		}
	}
	// no stand-in: unknown as before
	StandIn = func(string, string) string { return "" }
	if code, _ := post(t, "/v1/chat/completions", `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}`); code != 404 {
		t.Fatalf("without a stand-in: %d", code)
	}
}

// Codex's auto-review asks uni-switch, its provider, for "codex-auto-review":
// the review goes to the model that stands in (Codex's, TestCodexStandIn).
func TestCodexAutoReviewStandIn(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"c1","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`}
	setup(t, provider.Chat, f)
	StandIn = func(agent, model string) string {
		if model == "codex-auto-review" {
			return "fake/m1"
		}
		return ""
	}
	t.Cleanup(func() { StandIn = nil })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"codex-auto-review","input":"review this","stream":false}`))
	req.Header.Set("User-Agent", "codex_cli_rs/0.160.0 (Mac OS 26.6.0; arm64) Apple_Terminal/455")
	req.Header.Set("x-openai-subagent", "guardian")
	New().Handler().ServeHTTP(rec, req)
	if rec.Code == 404 || modelOf(f.got) != "m1" {
		t.Fatalf("auto-review: %d %s, upstream %s", rec.Code, rec.Body.String(), f.got)
	}
}

// Claude Code (here as T3 Code runs it) names each of Anthropic's models by
// its full id; every id of a family goes to the model Claude Code's tier for
// that family is set to, whether or not a provider of the user's serves the
// id itself (KevinXC on Discord: claude-sonnet-4-6 went to the sonnet tier,
// claude-sonnet-5 and claude-fable-5-1 to the Claude account, spent, 429).
func TestClaudeTierStandInByFamily(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`}
	up := setup(t, provider.Anthropic, f)
	p, _ := provider.Find("fake")
	p.Models = []string{"main", "flash", "sol", "astra", "grok"}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	// the user's Claude account, as uni-switch serves it, by Anthropic's ids
	native := []string{"claude-sonnet-5", "claude-sonnet-5-5", "claude-fable-5-1", "claude-fable-5", "claude-opus-5", "claude-opus-5-5", "claude-sonnet-4-6"}
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: up.URL, Models: native}); err != nil {
		t.Fatal(err)
	}
	tiers := map[string]string{"opus": "fake/sol:high[1m]", "sonnet": "fake/grok", "haiku": "fake/flash[1m]", "fable": "fake/astra[1m]"}
	StandIn = func(agent, model string) string {
		if agent != "claude" {
			return ""
		}
		for _, t := range []string{"opus", "sonnet", "haiku", "fable"} {
			if strings.Contains(strings.ToLower(model), t) {
				return tiers[t]
			}
		}
		return "fake/main"
	}
	t.Cleanup(func() { StandIn = nil })
	ask := func(model, ua string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"`+model+`","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
		if ua != "" {
			// Claude Code, known here by its token, as the agents
			// package isn't there to name its User-Agent
			req.Header.Set("User-Agent", ua)
			req.Header.Set("x-api-key", TokenFor("claude"))
		}
		New().Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	const cc = "claude-cli/2.1.287 (external, sdk-ts)"
	for asked, want := range map[string]string{
		"claude-haiku-4-5":            "flash",
		"claude-haiku-4-5-20251001":   "flash",
		"claude-haiku-5":              "flash",
		"claude-opus-5":               "sol",
		"claude-opus-5-5":             "sol",
		"claude-opus-5-5[1m]":         "sol",
		"claude-opus-6":               "sol", // no provider's: the tier, at its effort
		"claude-fable-5":              "astra",
		"claude-fable-5-1":            "astra",
		"claude-fable-5-1[1m]":        "astra",
		"claude-sonnet-4-6":           "grok",
		"claude-sonnet-5":             "grok",
		"claude-sonnet-5-5":           "grok",
		"claude-sonnet-5-5-20261001":  "grok",
		"claude-3-5-sonnet-20241022":  "grok",
		"anth/claude-sonnet-5":        "claude-sonnet-5", // a provider's model, picked by name
		"fake/main":                   "main",
		"claude-sonnet-5-5:something": "grok",
	} {
		code, body := ask(asked, cc)
		if code != 200 || modelOf(f.got) != want {
			t.Errorf("%s: %d %s, upstream got %q, want %q", asked, code, body, modelOf(f.got), want)
		}
	}
	// another agent's request for a model uni-switch serves is sent as asked
	if code, body := ask("claude-sonnet-5", ""); code != 200 || modelOf(f.got) != "claude-sonnet-5" {
		t.Errorf("another agent: %d %s, upstream got %q", code, body, modelOf(f.got))
	}
	// Claude Code not routed through uni-switch: nothing stands in
	StandIn = func(string, string) string { return "" }
	if code, body := ask("claude-fable-5-1", cc); code != 200 || modelOf(f.got) != "claude-fable-5-1" {
		t.Errorf("not routed: %d %s, upstream got %q", code, body, modelOf(f.got))
	}
}

// tbxark on X: a script Claude Code starts (a hook, its statusline, a
// plugin's) inherits its ANTHROPIC_BASE_URL and token, and asked uni-switch
// for claude-sonnet-4-6 as Python-urllib: 404 unknown model, while Claude
// Code's own requests went to its tier. A client uni-switch knows as no agent,
// with the plain token, naming a Claude id uni-switch doesn't serve, gets
// Claude Code's tier, on /v1/messages and count_tokens; an id the user
// serves goes where it is served, and an agent uni-switch gave a token keeps
// what it asked for.
func TestClaudeTierForAScriptClaudeCodeStarted(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`}
	up := setup(t, provider.Anthropic, f)
	p, _ := provider.Find("fake")
	p.Models = []string{"main", "flash", "grok", "sol"}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: up.URL, Models: []string{"claude-sonnet-5"}}); err != nil {
		t.Fatal(err)
	}
	var askedFor []string
	StandIn = func(agent, model string) string {
		askedFor = append(askedFor, agent)
		if agent != "claude" {
			return ""
		}
		switch {
		case strings.Contains(model, "sonnet"):
			return "fake/grok"
		case strings.Contains(model, "opus"):
			return "fake/sol"
		}
		return "fake/main"
	}
	t.Cleanup(func() { StandIn = nil })
	ask := func(path, model, ua, key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"`+model+`","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("User-Agent", ua)
		req.Header.Set("x-api-key", key)
		New().Handler().ServeHTTP(rec, req)
		return rec
	}
	const urllib = "Python-urllib/3.12"
	for asked, want := range map[string]string{"claude-sonnet-4-6": "grok", "claude-opus-5-5": "sol"} {
		if rec := ask("/v1/messages", asked, urllib, Token); rec.Code != 200 || modelOf(f.got) != want {
			t.Errorf("%s from a script: %d %s, upstream got %q, want %q", asked, rec.Code, rec.Body.String(), modelOf(f.got), want)
		}
	}
	for _, ua := range []string{"", "anthropic-python/0.75.0"} {
		if rec := ask("/v1/messages", "claude-sonnet-4-6", ua, Token); rec.Code != 200 || modelOf(f.got) != "grok" {
			t.Errorf("%q: %d %s, upstream got %q", ua, rec.Code, rec.Body.String(), modelOf(f.got))
		}
	}
	// count_tokens is counted for the model the turn goes to
	f.got = nil
	if rec := ask("/v1/messages/count_tokens", "claude-sonnet-4-6", urllib, Token); rec.Code != 200 || modelOf(f.got) != "grok" {
		t.Errorf("count_tokens from a script: %d %s, upstream got %q", rec.Code, rec.Body.String(), f.got)
	}
	// a Claude id the user serves is sent as asked
	if rec := ask("/v1/messages", "claude-sonnet-5", urllib, Token); rec.Code != 200 || modelOf(f.got) != "claude-sonnet-5" {
		t.Errorf("served id: %d %s, upstream got %q", rec.Code, rec.Body.String(), modelOf(f.got))
	}
	// an agent uni-switch gave its own token isn't taken for Claude Code
	askedFor = nil
	if rec := ask("/v1/messages", "claude-sonnet-4-6", urllib, TokenFor("codex")); rec.Code != 404 || slices.Contains(askedFor, "claude") {
		t.Errorf("an agent's token: %d %s, stand-ins asked %v", rec.Code, rec.Body.String(), askedFor)
	}
	// Claude Code not routed through uni-switch: still unknown
	StandIn = func(string, string) string { return "" }
	if rec := ask("/v1/messages", "claude-sonnet-4-6", urllib, Token); rec.Code != 404 {
		t.Errorf("not routed: %d %s", rec.Code, rec.Body.String())
	}
}
