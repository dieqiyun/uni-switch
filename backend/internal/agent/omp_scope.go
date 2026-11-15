package agent

import (
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"gopkg.in/yaml.v3"
)

// omp's enabledModels (config.yml) is, as Pi's is (pi.go), the list a new
// session is held to while it names anything: the default role starts only
// when the list takes its model in, else omp picks another model from the
// list (main.ts buildSessionOptions, sdk.ts resolveAllowedModels and
// pickDefaultAvailableModel), so a model uni-switch picks outside it never
// starts (#1769). omp's other roles are resolved against every model it has
// (task/executor.ts resolveModelOverride, session/model-controls.ts
// resolveRoleModelFull, both on modelRegistry.getAvailable()), so only the
// default role is held to the list.
//
// The list is the user's: uni-switch adds the default role's model to it as Pi's
// fix did (#160), keeps the entries, their order and comments,
// and never makes a list where there is none or an empty one. Its plain
// entries hold in every folder; an entry scoped to folders ({path, models})
// is the user's list for those projects and left as it is.

// ompScopePlain are the plain entries of an enabledModels node.
func ompScopePlain(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode && c.ShortTag() == "!!str" {
			out = append(out, c.Value)
		}
	}
	return out
}

// ompEvery is the entry uni-switch adds to enabledModels for its own models:
// every model uni-switch writes into models.yml for omp, which are the ones its
// Agents page offers. omp matches a glob with Bun.Glob, whose * stops at a
// slash, and a model of uni-switch's is uni-switch/<provider>/<model>, so it takes
// **. With the default role's model alone there, omp's picker and Ctrl+P
// left out every other model uni-switch gives it (#1769, real-jacket).
const ompEvery = uniSwitchID + "/**"

// ompTakes reports whether an enabledModels entry surely takes in model, a
// provider/model with no level: Pi's rules (piTakes), with the levels omp
// reads off an entry's end, auto among them, and a trailing ** taking in
// everything under it, slashes and all, as Bun.Glob's does.
func ompTakes(pattern, model string) bool {
	p, _ := ompLevel(strings.TrimSpace(pattern))
	if base, ok := strings.CutSuffix(p, "**"); ok && !strings.ContainsAny(base, "*?[") && (base == "" || strings.HasSuffix(base, "/")) {
		return strings.HasPrefix(strings.ToLower(model), strings.ToLower(base))
	}
	return piTakes(p, model)
}

// ompScopeTakes says whether the plain entries take in model; true when
// there are none, as then nothing holds a session to them.
func ompScopeTakes(plain []string, model string) bool {
	if len(plain) == 0 {
		return true
	}
	for _, p := range plain {
		if ompTakes(p, model) {
			return true
		}
	}
	return false
}

// ompScopeOwnuni-switch says whether the user chose for themselves which of
// uni-switch's models the list holds: an entry on uni-switch with a glob or a
// thinking level, or a glob of theirs that takes model in (deepseek/*,
// which omp matches against the id alone), is theirs, not one uni-switch added.
func ompScopeOwnUniSwitch(plain []string, model string) bool {
	for _, p := range plain {
		m, level := ompLevel(strings.TrimSpace(p))
		glob := strings.ContainsAny(m, "*?[")
		if glob && ompTakes(m, model) || strings.HasPrefix(strings.ToLower(m), uniSwitchID+"/") && (glob || level != "") {
			return true
		}
	}
	return false
}

// ompScopeWith lets a session start on model, a provider/model, when
// config.yml's enabledModels has plain entries and none takes it in. For
// one of uni-switch's it is ompEvery that joins the end, in place of the
// models of uni-switch's the list names one by one (what uni-switch added before),
// so omp's picker has every model uni-switch offers it beside the user's own;
// unless the user picked which of uni-switch's the list holds
// (ompScopeOwnuni-switch), when only model joins it. Another model joins as
// itself.
func ompScopeWith(cfg, model string) error {
	return edit.EditYAMLNode(cfg, "enabledModels", func(n *yaml.Node) (bool, bool) {
		plain := ompScopePlain(n)
		if len(plain) == 0 {
			return false, false
		}
		onUniSwitch := strings.HasPrefix(strings.ToLower(model), uniSwitchID+"/")
		if !onUniSwitch || ompScopeOwnUniSwitch(plain, model) {
			if ompScopeTakes(plain, model) {
				return false, false
			}
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: model})
			return true, false
		}
		kept := n.Content[:0:0]
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && ompScopeUniSwitchOne(c.Value) {
				continue
			}
			kept = append(kept, c)
		}
		n.Content = append(kept, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ompEvery})
		return true, false
	})
}

// ompScopeUniSwitchOne is a plain entry naming one of uni-switch's models, with
// no glob or level: what uni-switch adds, or ompEvery takes in all the same.
func ompScopeUniSwitchOne(v string) bool {
	v = strings.TrimSpace(v)
	return !strings.ContainsAny(v, "*?[") && strings.HasPrefix(strings.ToLower(v), uniSwitchID+"/")
}

// ompScopeWithout takes uni-switch's models out of enabledModels's plain
// entries, ompEvery among them, once nothing else in config.yml is on
// uni-switch, and its provider leaves models.yml: there they would name
// nothing. A list left empty is removed, never written as [].
func ompScopeWithout(cfg string) error {
	return edit.EditYAMLNode(cfg, "enabledModels", func(n *yaml.Node) (bool, bool) {
		if n == nil || n.Kind != yaml.SequenceNode {
			return false, false
		}
		kept := n.Content[:0:0]
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && (strings.EqualFold(strings.TrimSpace(c.Value), ompEvery) ||
				!strings.ContainsAny(c.Value, "*?[") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Value)), uniSwitchID+"/")) {
				continue
			}
			kept = append(kept, c)
		}
		if len(kept) == len(n.Content) {
			return false, false
		}
		if len(kept) == 0 {
			return false, true
		}
		n.Content = kept
		return true, false
	})
}

// ompScopeOnce drops a plain entry of enabledModels that an earlier one
// spells the same: a provider renamed has the default role, set again under
// the new id, join the list (ompScopeWith) before the old entry is renamed
// to it.
func ompScopeOnce(cfg string) error {
	return edit.EditYAMLNode(cfg, "enabledModels", func(n *yaml.Node) (bool, bool) {
		if n == nil || n.Kind != yaml.SequenceNode {
			return false, false
		}
		seen := map[string]bool{}
		kept := n.Content[:0:0]
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode {
				if seen[c.Value] {
					continue
				}
				seen[c.Value] = true
			}
			kept = append(kept, c)
		}
		if len(kept) == len(n.Content) {
			return false, false
		}
		n.Content = kept
		return true, false
	})
}

// ompScopeFoldersOnuni-switch says whether an entry of enabledModels scoped to
// folders names one of uni-switch's models: the user's own, which keeps
// uni-switch's provider as a role on it does.
func ompScopeFoldersOnUniSwitch(cfg string) (used bool, err error) {
	err = edit.EditYAMLNode(cfg, "enabledModels", func(n *yaml.Node) (bool, bool) {
		if n == nil || n.Kind != yaml.SequenceNode {
			return false, false
		}
		for _, c := range n.Content {
			if c.Kind != yaml.MappingNode {
				continue
			}
			var walk func(*yaml.Node)
			walk = func(n *yaml.Node) {
				if n.Kind == yaml.ScalarNode {
					ompRefs(n.Value, func(m string) string { used = used || usesUniSwitch(m); return m })
				}
				for _, c := range n.Content {
					walk(c)
				}
			}
			walk(c)
		}
		return false, false
	})
	return used, err
}

// ompScopeLeavesOut is the default role's model when enabledModels has
// plain entries and none takes it in: a new session would start on another.
func ompScopeLeavesOut(cfg, role string) string {
	model, _, one := ompSplit(role)
	if !one || model == "" || ompScopeTakes(edit.GetYAMLList(cfg, "enabledModels"), model) {
		return ""
	}
	return model
}
