package gateway

import "testing"

// Antigravity answers a 429 "Resource has been exhausted (e.g. check
// quota)" to Claude Code's and the Agent SDK's system prompt whatever the
// quota left (#666: Claude Desktop's chats always 429, its titles and
// Codex on the same account fine); uni-switch says so rather than leave a user
// chasing a quota. Both lines are as Claude Code 2.1.288 sends them.
func TestAntigravityTurnsAwayClaudeCodesSystemPrompt(t *testing.T) {
	for _, system := range []string{
		"x-anthropic-billing-header: cc_version=2.1.288.e3f; cc_entrypoint=sdk-cli;\nYou are Claude Code, Anthropic's official CLI for Claude.",
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.\n\nYou are an interactive agent …",
		"Some instructions first.\nYou are a Claude agent, built on Anthropic's Claude Agent SDK.",
	} {
		if !antigravityTurnsAway(system) {
			t.Errorf("%q isn't taken for what Antigravity turns away", system)
		}
	}
	// what it lets through: other agents', Claude Code's own name alone,
	// and no system prompt at all (Claude Desktop's title requests)
	for _, system := range []string{
		"",
		"You are a helpful assistant.",
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"You are Codex, based on GPT-5.",
	} {
		if antigravityTurnsAway(system) {
			t.Errorf("%q is taken for what Antigravity turns away; it answers it", system)
		}
	}
}
