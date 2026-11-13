package provider

// A group's member may be sent in its vendor's fast mode (Discord: "gpt-6.1
// sol · high · fast"): OpenAI's priority processing (service_tier
// "priority", Codex's Fast) on a ChatGPT account or an OpenAI key, Cursor's
// -fast model, Claude's fast mode (speed "fast") on an Anthropic key for
// the Opus models that have it. A member whose model has none is sent as
// it is.
//
// Group.Fast lists those members as Members spells them, kept apart from
// the member's id so that a version before it, which reads no "fast",
// still routes to the member, only not fast. Typed, it is a last part
// ":fast" after the effort, if any: "codex/gpt-6.1-sol:high:fast".

import (
	"regexp"
	"slices"
	"strings"
)

// FastWord is the last part of a member typed to be sent fast.
const FastWord = "fast"

// CanFast reports whether model, as p serves it, has a fast mode uni-switch
// can ask for: a ChatGPT account's GPT models (Codex's Fast), Cursor's
// models with a -fast one, an OpenAI key's GPT and o-series models on its
// Responses API (priority processing), an Anthropic key's Opus models with
// fast mode, and a plugin's models it says are served at priority.
// A Claude subscription runs Claude Code itself, and a cloud (Bedrock,
// Vertex) refuses the field, so neither is. A provider the user added by
// its address (AddedByAddress) is asked for priority on a GPT model it
// serves on an OpenAI API, when the user switches it fast themselves
// (#1615): it is sent service_tier "priority" there, as Codex's own Fast
// already is (#1234), and one that turns the field away is asked again
// without it and not sent it again (the gateway's optional fields).
func CanFast(p Provider, model string) bool {
	if p.IsPlugin() {
		pp, ok := PluginOf(p.ID)
		if !ok {
			return false
		}
		// a model the plugin says its vendor serves at the priority tier
		// (serviceTiers, #2514), on its Chat or Responses API, which the
		// gateway sends service_tier "priority" there (Lovest on Discord:
		// only Cursor's plugin could be picked fast)
		m, listed := pluginModel(pp, model)
		if listed && pluginTierOn(pp.ID, m, "priority") {
			return true
		}
		// Cursor's plugin runs a model as its fast variant when the chat
		// request says service_tier "priority". From 0.2.2 it lists a model
		// once per context size (claude-opus-5-5@300k) and says fast on
		// the ones Cursor has a fast variant of (#1360); 0.1.x listed the
		// fast one as a model of its own, -fast, as the built-in does
		if p.PluginProvider() != "cursor" {
			return false
		}
		if listed && m.Fast {
			return true
		}
		if strings.HasSuffix(model, "-fast") {
			return false
		}
		_, ok = pluginModel(pp, model+"-fast")
		return ok
	}

	// another uni-switch's model it offers its own Codex Fast on (its
	// list's service_tiers, #1234): a pick sent fast here goes on to
	// it with service_tier "priority", which it sends its vendor as
	// its own Codex's turn would be (littlekk88 on Discord: the
	// picker had no Fast for a remote uni-switch's model)

	if p.Account != nil {
		switch p.Account.Agent {
		case "codex":
			return strings.HasPrefix(model, "gpt-")
		case "cursor":
			if strings.HasSuffix(model, "-fast") {
				return false
			}
			_, ok := CursorVariants(model + "-fast")
			return ok
		}
		return false
	}
	if p.IsBedrock() {
		return false
	}
	// on OpenAI's Responses API: a request translated for its Chat API is
	// told no tier (TestCursorPluginFast)
	if HostOf(p.Responses) == "api.openai.com" && openAIFast.MatchString(model) {
		return true
	}
	if HostOf(p.Anthropic) == "api.anthropic.com" && ClaudeFast(model) {
		return true
	}
	return OwnFast(p, model)
}

// OwnFast reports whether model has a fast mode only as the user's own
// provider may serve it (#1615): a GPT model on a provider they added by
// its address, on an OpenAI API, which uni-switch can't tell serves priority
// and asks for it all the same when the user switches it fast. Not where
// CanFast knows the vendor's (OpenAI's own API).
func OwnFast(p Provider, model string) bool {
	if !p.AddedByAddress() || p.IsBedrock() || HostOf(p.Responses) == "api.openai.com" {
		return false
	}
	return (p.Responses != "" || p.Chat != "") && openAIFast.MatchString(modelSlug(model))
}

// AddedByAddress reports whether the user added the provider by its
// address: no preset, no account, no plugin. The tier an agent asks for
// goes to it as it was asked (the gateway's OwnTier).
func (p Provider) AddedByAddress() bool {
	return p.ID != "" && p.Preset == "" && p.Account == nil && !p.IsPlugin()
}

// modelSlug is a model's id as its own vendor names it, as a provider the
// user added may serve it: openai/GPT-6-sol is gpt-6-sol.
func modelSlug(model string) string {
	model = strings.ToLower(model)
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

var (
	openAIFast = regexp.MustCompile(`^(gpt-|o\d)`)
	// a dated id is its model's: claude-opus-4-8-20260501
	claudeDated = regexp.MustCompile(`-\d{8}$`)
)

// claudeFastModels are the Claude models with fast mode (speed "fast",
// beta fast-mode-2026-02-01).
var claudeFastModels = []string{"claude-opus-4-8", "claude-opus-5", "claude-opus-5-5"}

// ClaudeFast reports whether a Claude model has fast mode.
func ClaudeFast(model string) bool {
	return slices.Contains(claudeFastModels, claudeDated.ReplaceAllString(strings.ToLower(model), ""))
}
