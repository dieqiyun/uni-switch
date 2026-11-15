package agent

// fx (fx.sh, Vercel Labs) keeps its settings in ~/.fx/settings.json: the
// provider it talks to under "provider" (gateway, codex, grok or one of
// the user's own), each provider's model under "models", and providers of
// the user's own under "providers". uni-switch adds itself there as the
// provider "uni-switch", keyless (auth none), speaking chat completions, with
// the catalog as its model_metadata; a model through uni-switch is
// "uni-switch/<provider>/<model>" here, provider "uni-switch" and models.uni-switch
// "<provider>/<model>" in fx's settings. fx sends no effort to a provider
// of the user's own, so there is no effort to pick.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// fxMaxModels is how many models fx takes in a provider's model_metadata;
// past it fx refuses the whole settings file.
const fxMaxModels = 256

func fx(home string) *Agent { return fxIn(here(home)) }

// fxIn is fx at a place: this machine's home, or a WSL distro's (see
// wsl.go), its provider naming the gateway as the distro reaches it.
func fxIn(at place) *Agent {
	dir := at.join(".fx")
	path := filepath.Join(dir, "settings.json")
	// uni-switch's provider, with what the user set on it in fx kept
	fxProvider := func(cur string) any { return fxKept(path, fxProviderAt(cur, at.v1())) }
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	// the provider fx talks to; unset, it is Vercel's AI Gateway
	prov := func() string {
		if p := get("provider"); p != "" {
			return p
		}
		return "gateway"
	}
	// own is the model fx has for one of its own providers: the gateway's
	// is also kept as the top-level "model", which models.gateway overrides
	own := func(p string) string {
		if v := fxModels(path)[p]; v != "" || p != "gateway" {
			return v
		}
		return get("model")
	}
	dropUniSwitch := func() error {
		keys := []string{"providers." + uniSwitchID, "models." + uniSwitchID}
		if get("provider") == uniSwitchID {
			keys = append(keys, "provider")
		}
		return edit.DelJSON(path, keys...)
	}
	return &Agent{
		ID: "fx", Name: "fx", Icon: "fx", Spelled: prefixed, NoNames: true,
		Dir: dir, Path: path,
		// ~/.fx is where its settings are. No command: `fx` is a common name
		// (the JSON viewer has it too), as in WSL (wsl.go).
		detect:   func() bool { return agentDir(dir) },
		ListOnly: true,
		Sync: func() error {
			return syncJSON(path, "providers."+uniSwitchID, func() any { return fxProvider(fxModels(path)[uniSwitchID]) })
		},
		Notice: func() string {
			if Running(`(^|/)fx( |$)`) {
				return noticeFx.String()
			}
			return ""
		},
		Check: func() string {
			if prov() != uniSwitchID {
				return ""
			}
			return wiringOff("fx", path, func(k string) (string, bool) { return edit.GetJSON(path, "providers."+uniSwitchID+"."+k) },
				"base_url", at.v1())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: func() string {
				if p := prov(); p != uniSwitchID {
					return own(p)
				}
				if m := fxModels(path)[uniSwitchID]; m != "" {
					return uniSwitchID + "/" + m
				}
				return ""
			},
			Set: func(v string) error {
				if ref, ok := strings.CutPrefix(v, uniSwitchID+"/"); ok && isUniSwitch(ref) {
					return edit.SetJSON(path,
						edit.KV{Path: "providers." + uniSwitchID, Value: fxProvider(ref)},
						edit.KV{Path: "provider", Value: uniSwitchID},
						edit.KV{Path: "models." + uniSwitchID, Value: ref})
				}
				// fx's own: uni-switch steps out, back to the provider fx
				// talks to without it
				if err := dropUniSwitch(); err != nil {
					return err
				}
				p := prov()
				if v == "" {
					keys := []string{"models." + p}
					if p == "gateway" {
						keys = append(keys, "model")
					}
					return edit.DelJSON(path, keys...)
				}
				return edit.SetJSON(path, edit.KV{Path: "models." + p, Value: v})
			},
			Options: func(cur map[string]string) []Option {
				var out []Option
				if v := cur["model"]; v != "" && !usesUniSwitch(v) {
					out = append(out, Option{Value: v, Icon: modelIcon("", v)})
				}
				return withVia(out, "fx", uniSwitchID+"/")
			},
		}},
	}
}

// fxModels is settings.json's models, provider to model.
func fxModels(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var c struct {
		Models map[string]any `json:"models"`
	}
	if json.Unmarshal(b, &c) != nil {
		return out
	}
	for k, v := range c.Models {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// fxProviderAt is uni-switch's entry in settings.json's providers, for an fx
// reaching the gateway's /v1 at v1. Its model_metadata is what fx's model
// picker lists: the catalog, the model in use first so it is never the one
// cut at fx's limit.
func fxProviderAt(cur, v1 string) map[string]any {
	all := uniSwitchModels("fx")
	slices.SortStableFunc(all, func(a, b catalog.Model) int {
		switch {
		case a.ID == cur && b.ID != cur:
			return -1
		case b.ID == cur && a.ID != cur:
			return 1
		}
		return 0
	})
	ms := map[string]any{}
	for _, m := range all {
		if len(ms) == fxMaxModels {
			break
		}
		e := map[string]any{"supports_tool_use": true, "supports_vision": m.Images}
		if m.Context > 0 {
			e["context_window"] = m.Context
		}
		// fx refuses an output limit that isn't below the context window
		if m.Output > 0 && (m.Context == 0 || m.Output < m.Context) {
			e["max_output_tokens"] = m.Output
		}
		ms[m.ID] = e
	}
	return map[string]any{
		"protocol": "openai-chat-completions", "base_url": v1, "auth": map[string]any{"type": "none"},
		"tool_choice_mode": "send", "model_metadata": ms,
	}
}

// fxKept is want, uni-switch's provider in fx's settings.json at path, with
// what the user set on the one there kept: fx takes a reviewer_model on a
// provider (the model its auto permission mode asks), which uni-switch doesn't
// write, and a tool_choice_mode, which uni-switch writes "send" and the user
// may turn to "omit" (src/core/config/configured_provider.zig); a sync wrote
// both away on every start, as T3 Code's did (#2030). Its protocol,
// address, auth and model_metadata are uni-switch's; auth is written whole, as
// fx refuses an env beside type none.
func fxKept(path string, want map[string]any) any {
	key := "providers." + uniSwitchID
	if mode, ok := edit.GetJSON(path, key+".tool_choice_mode"); ok && (mode == "omit" || mode == "send") {
		want["tool_choice_mode"] = mode
	}
	return theirsKept(path, key, func() any { return want }, "model_metadata", "auth")()
}

// what fx says after a change (notice.go)
var (
	noticeFx = newNotice("fx reads its settings at start-up — restart open fx sessions to use this.")
)
