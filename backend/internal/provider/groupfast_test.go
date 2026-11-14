package provider

import (
	"testing"
)

// fastHome is a machine of its own with an OpenAI key, an Anthropic key, a
// relay and a ChatGPT account.
func fastHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []Provider{
		{ID: "oa", Name: "OpenAI", Key: "k", Responses: "https://api.openai.com/v1", Models: []string{"gpt-6.1-sol", "text-embedding-3"}},
		{ID: "an", Name: "Anthropic", Key: "k", Anthropic: "https://api.anthropic.com", Models: []string{"claude-opus-5-5", "claude-opus-4-8-20260501", "claude-sonnet-5"}},
		{ID: "rl", Name: "Relay", Key: "k", Chat: "https://relay.example/v1", Anthropic: "https://relay.example", Models: []string{"gpt-6.1-sol", "claude-opus-5-5", "odd:fast"}},
	} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanFast(t *testing.T) {
	fastHome(t)
	codex := Provider{ID: "codex", Account: &Account{Agent: "codex"}}
	claude := Provider{ID: "claude", Account: &Account{Agent: "claude"}, Anthropic: "https://api.anthropic.com"}
	oa, _ := Find("oa")
	an, _ := Find("an")
	rl, _ := Find("rl")
	for _, c := range []struct {
		p     Provider
		model string
		want  bool
	}{
		{codex, "gpt-6.1-sol", true},
		{claude, "claude-opus-5-5", false}, // Claude Code's own
		{*oa, "gpt-6.1-sol", true},
		{*oa, "o4-mini", true},
		{*oa, "text-embedding-3", false},
		{*an, "claude-opus-5-5", true},
		{*an, "claude-opus-4-8-20260501", true},
		{*an, "claude-sonnet-5", false},
		{*rl, "claude-opus-5-5", false}, // Claude's speed isn't asked of a relay
	} {
		if got := CanFast(c.p, c.model); got != c.want {
			t.Errorf("%s/%s: %v, want %v", c.p.ID, c.model, got, c.want)
		}
		if OwnFast(c.p, c.model) {
			t.Errorf("%s/%s: the vendor's own fast mode taken for the user's provider's", c.p.ID, c.model)
		}
	}
}

// A GPT model on a provider the user added by its address may be switched
// fast (#1615, iomect: 目前只有官key可以开启快速模式): it is asked for
// priority on its OpenAI API. Not on a preset's, a cloud's, an account's,
// another model's, or one served only on Anthropic's API.
func TestOwnFast(t *testing.T) {
	fastHome(t)
	rl, _ := Find("rl")
	preset := *rl
	preset.ID, preset.Preset = "or", "openrouter"
	resp := Provider{ID: "sub", Responses: "https://sub2api.example/v1", Models: []string{"gpt-6.1-sol"}}
	for _, c := range []struct {
		p     Provider
		model string
		want  bool
	}{
		{*rl, "gpt-6.1-sol", true},
		{resp, "gpt-6.1-sol", true},
		{resp, "openai/GPT-6.1-sol", true},
		{resp, "o4-mini", true},
		{Provider{ID: "oc", Chat: "https://api.openai.com/v1"}, "gpt-6.1-sol", true}, // OpenAI's Chat API added by its address
		{*rl, "odd:fast", false},
		{*rl, "glm-5", false},
		{*rl, "claude-opus-5-5", false},
		{preset, "gpt-6.1-sol", false},
		{Provider{ID: "ra", Anthropic: "https://relay.example", Models: []string{"gpt-6.1-sol"}}, "gpt-6.1-sol", false},
		{Provider{ID: "br", Chat: "https://bedrock-runtime.us-east-1.amazonaws.com/v1"}, "gpt-6.1-sol", false},
		{Provider{ID: "codex", Account: &Account{Agent: "codex"}, Chat: "https://relay.example/v1"}, "gpt-6.1-sol", false},
	} {
		if got := OwnFast(c.p, c.model); got != c.want {
			t.Errorf("OwnFast %s/%s: %v, want %v", c.p.ID, c.model, got, c.want)
		}
		if c.want && !CanFast(c.p, c.model) {
			t.Errorf("CanFast %s/%s: false", c.p.ID, c.model)
		}
	}
}
