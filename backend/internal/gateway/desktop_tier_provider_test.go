package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// cybercaius on Discord: Claude Desktop's chat was on AgentRouter's
// gpt-6.1-sol, and Claude Code's haiku calls in it (its small, fast
// model) went to Leee's claude-haiku-5-5, a provider the user never picked
// in Desktop. /v1/models tagged Leee's haiku with the haiku tier unasked,
// so Claude Code asked for it by name. Untagged, Claude Code asks for its
// own claude-haiku id, which stays on the chat's provider: its model of
// that tier when it has one, else the chat's model. A tier the user picked
// keeps its pick.
func TestClaudeDesktopBackgroundStaysOnChatProvider(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desktopPicked.Lock()
	desktopPicked.model, desktopPicked.from = "", ""
	desktopPicked.Unlock()
	for _, p := range []provider.Provider{
		{ID: "agentrouter", Name: "AgentRouter", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"gpt-6.1-sol"}},
		{ID: "leee", Name: "Leee", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-haiku-5-5"}},
		{ID: "claudeish", Name: "Claudeish", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-opus-5-5", "claude-haiku-4-5", "claude-haiku-5-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range desktopModels(provider.Catalog(), true) {
		if tier, _ := m["anthropic_family_tier"].(string); tier != "" {
			t.Fatalf("%v tagged %q with nothing picked", m["id"], tier)
		}
	}

	turn := []byte(`{"max_tokens":32000,"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"hi"}]}`)
	small := []byte(`{"max_tokens":3000,"system":[{"type":"text","text":"Summarize."}],"messages":[{"role":"user","content":"…"}]}`)
	sub := []byte(`{"max_tokens":32000,"tools":[{"name":"Read"}],"messages":[{"role":"user","content":"look"}]}`)

	// the chat on AgentRouter's GPT: Claude Code's haiku calls go there,
	// though Leee serves claude-haiku-5-5 by that very name
	if got := desktopTurn("agentrouter/gpt-6.1-sol", turn, true, false); got != "agentrouter/gpt-6.1-sol" {
		t.Fatalf("turn sent %q", got)
	}
	for _, c := range []struct {
		asked string
		body  []byte
		sub   bool
	}{
		{"claude-haiku-5-5", small, false},
		{"claude-haiku-4-5-20251001", small, false},
		{"claude-haiku-5-5", sub, true},
	} {
		if got := desktopTurn(c.asked, c.body, true, c.sub); got != "agentrouter/gpt-6.1-sol" {
			t.Errorf("chat on AgentRouter, %s (subagent %v) sent to %q", c.asked, c.sub, got)
		}
	}

	// the chat on a provider with haikus of its own: its newest haiku,
	// or the very one asked for when it has it
	if got := desktopTurn("claudeish/claude-opus-5-5", turn, true, false); got != "claudeish/claude-opus-5-5" {
		t.Fatalf("turn sent %q", got)
	}
	for asked, want := range map[string]string{
		"claude-haiku-5-5":          "claudeish/claude-haiku-5-5",
		"claude-haiku-4-5":          "claudeish/claude-haiku-4-5",
		"claude-haiku-4-5-20251001": "claudeish/claude-haiku-5-5",
		"claude-haiku-9[1m]":        "claudeish/claude-haiku-5-5[1m]",
	} {
		if got := desktopTurn(asked, small, true, false); got != want {
			t.Errorf("chat on Claudeish, %s sent to %q, want %q", asked, got, want)
		}
	}

	// a haiku the user picked for the tier is tagged and kept
	if err := SetDesktopTier("haiku", "leee/claude-haiku-5-5"); err != nil {
		t.Fatal(err)
	}
	tagged := false
	for _, m := range desktopModels(provider.Catalog(), true) {
		if DesktopCatalogID(m["id"].(string)) == "leee/claude-haiku-5-5" {
			tagged = m["anthropic_family_tier"] == "haiku" && m["is_family_default"] == true
		}
	}
	if !tagged {
		t.Fatal("the picked haiku isn't tagged")
	}
	if got := desktopTurn("leee/claude-haiku-5-5", small, true, false); got != "leee/claude-haiku-5-5" {
		t.Fatalf("the picked haiku sent to %q", got)
	}
}

// CyberCaius on Discord: Claude Desktop's chat was on Leee's DeepSeek V4.1
// Flash, with no tier picked, and Claude Code's sonnet calls in it went to
// Leee's claude-sonnet-5-5, which Leee's key may not use (403). The user
// picked no Claude model: a chat on another vendor's model keeps Claude
// Code's tier calls, background or subagent, on the chat's model, though
// its provider lists Claude models. A chat on a Claude model keeps its
// provider's model of the tier (#2187), and a picked tier its pick.
func TestClaudeDesktopNonClaudeChatKeepsTierCalls(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desktopPicked.Lock()
	desktopPicked.model, desktopPicked.from = "", ""
	desktopPicked.Unlock()
	if err := provider.Save(provider.Provider{ID: "leee", Name: "Leee", Key: "k", Anthropic: "http://127.0.0.1:1",
		Models: []string{"deepseek-v4.1-flash-1017", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	turn := []byte(`{"max_tokens":32000,"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"hi"}]}`)
	classifier := []byte(`{"max_tokens":256,"messages":[{"role":"user","content":"is this safe?"}]}`)
	sub := []byte(`{"max_tokens":32000,"tools":[{"name":"Read"}],"messages":[{"role":"user","content":"look"}]}`)

	chat := "leee/deepseek-v4.1-flash-1017"
	if got := desktopTurn(chat, turn, true, false); got != chat {
		t.Fatalf("turn sent %q", got)
	}
	for _, c := range []struct {
		asked string
		body  []byte
		sub   bool
	}{
		{"claude-sonnet-5-5", classifier, false},
		{"claude-sonnet-5-5", sub, true},
		{"claude-haiku-5-5", classifier, false},
		{"claude-haiku-4-5-20251001", classifier, false},
		{"claude-opus-5-5[1m]", sub, true},
	} {
		if got := desktopTurn(c.asked, c.body, true, c.sub); got != chat {
			t.Errorf("chat on Leee's DeepSeek, %s (subagent %v) sent to %q, want the chat's model", c.asked, c.sub, got)
		}
	}

	// the chat on Leee's own Claude model: its sonnet stands in, as before
	if got := desktopTurn("leee/claude-opus-5-5", turn, true, false); got != "leee/claude-opus-5-5" {
		t.Fatalf("turn sent %q", got)
	}
	if got := desktopTurn("claude-sonnet-5-5", classifier, true, false); got != "leee/claude-sonnet-5-5" {
		t.Errorf("chat on Leee's Opus, sonnet call sent to %q, want leee/claude-sonnet-5-5", got)
	}

	// back on DeepSeek, a sonnet the user picked for the tier is kept,
	// asked for by its catalog id (tagged) or by Claude Code's own id,
	// which Leee lists by that name too
	if err := provider.Save(provider.Provider{ID: "other", Name: "Other", Key: "k", Anthropic: "http://127.0.0.1:1",
		Models: []string{"claude-sonnet-5"}}); err != nil {
		t.Fatal(err)
	}
	if got := desktopTurn(chat, turn, true, false); got != chat {
		t.Fatalf("turn sent %q", got)
	}
	if err := SetDesktopTier("sonnet", "other/claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	for _, asked := range []string{"other/claude-sonnet-5", "claude-sonnet-5-5"} {
		if got := desktopTurn(asked, classifier, true, false); got != "other/claude-sonnet-5" {
			t.Errorf("%s with sonnet picked sent to %q", asked, got)
		}
	}
}
