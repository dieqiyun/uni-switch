package provider

import (
	"fmt"
	"slices"
)

// CarriesCompaction says whether p's wire can carry Codex's own
// compaction (NativeCompaction, #1099): a compaction_trigger asked and a
// sealed compaction item given back, on the Responses API, as Codex sent
// them. A provider of no sign-in with a Responses API does (Sub2API and
// other relays of OpenAI's, OpenAI's own, a remote uni-switch), as do a
// ChatGPT account and GitHub Copilot, whose GPT models answer on
// Responses. Another signed-in agent's, or a plugin's, is asked in its own
// client's format, which has no such item.
func (p Provider) CarriesCompaction() bool {
	if p.Account != nil {
		return !p.IsPlugin() && (p.Account.Agent == "codex" || p.Account.Agent == "copilot") && p.Base(Responses) != ""
	}
	return p.Base(Responses) != ""
}

// CompactsOn says whether a Codex compaction for model at p goes to it as
// Codex sent it: the user turned NativeCompaction on, its wire carries
// it, and model is asked on Responses there (Copilot's Claude models are
// not). The gateway also asks whether the model fits Responses as it has
// seen it answer (Server.compactsNatively).
func (p Provider) CompactsOn(model string) bool {
	if !p.NativeCompaction || !p.CarriesCompaction() {
		return false
	}
	apis := p.APIs(model)
	return apis == nil && !p.NamedOnlyOn(Responses) || slices.Contains(apis, Responses)
}

// SetNativeCompaction turns a provider's native compaction on or off
// (Provider.NativeCompaction). One whose wire can't carry it is refused
// turning it on.
func SetNativeCompaction(id string, on bool) error {
	p, err := Find(id)
	if err != nil {
		return err
	}
	if on && !p.CarriesCompaction() {
		return fmt.Errorf("%s can't carry Codex's native compaction: only a provider with a Responses API, a ChatGPT account or GitHub Copilot can", p.Name)
	}
	p.NativeCompaction = on
	return Save(*p)
}
