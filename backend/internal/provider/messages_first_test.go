package provider

import "testing"

// The API a model is wired to agents on (Pi, omp, droid each speak the one
// uni-switch names for a model): a Claude model at a relay with an Anthropic URL
// beside its OpenAI one is Anthropic's Messages, not Chat, which drops its
// cache_control (#997). Other models, and a provider without an Anthropic
// URL, are as before.
func TestNativeClaudeIsMessagesWhereServed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	both := Provider{ID: "relay", Chat: "https://relay.example/v1", Anthropic: "https://relay.example"}
	every := Provider{ID: "relay", Chat: "https://relay.example/v1", Responses: "https://relay.example/v1", Anthropic: "https://relay.example"}
	chatOnly := Provider{ID: "relay", Chat: "https://relay.example/v1"}
	for _, c := range []struct {
		p     Provider
		model string
		want  Protocol
	}{
		{both, "claude-sonnet-4-5", Anthropic},
		{every, "claude-opus-4-1-20250805", Anthropic},
		{every, "anthropic/claude-sonnet-4.5", Anthropic},
		{both, "glm-5", Chat},
		{every, "gpt-5.1", Chat},
		{chatOnly, "claude-sonnet-4-5", Chat},
	} {
		if got := c.p.Native(c.model); got != c.want {
			t.Errorf("%+v Native(%s) = %q, want %q", c.p, c.model, got, c.want)
		}
	}
}
