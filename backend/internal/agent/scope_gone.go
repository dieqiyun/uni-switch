package agent

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"gopkg.in/yaml.v3"
)

// An enabledModels list (Pi's and its forks', StepCode's, omp's) names
// models as provider/model. An entry that names one outright, when no model
// has that name, gets "No models match pattern" from Pi every time it starts
// (model-resolver.js resolveModelScope); it never takes such an entry out.
// uni-switch's catalog sync takes a model out of the provider block it writes
// when it is deselected in a provider's editor, or its provider goes, and
// then the list's entries on it named nothing (#2055, misaka9981).
//
// After the sync, an entry that names one of uni-switch's models outright (no
// glob; a thinking level on its end is read off) whose model uni-switch no
// longer gives the agent leaves the list. It may be the user's own, so it is
// remembered, and goes back on the list's end once uni-switch gives that model
// again. Globs, entries on other providers and entries on a model uni-switch
// still gives stay as they are, in their order. A list left with nothing is
// removed, never written empty, and comes back with its entries. A list the
// user removed themselves isn't made again.

// scopeGone is what a sync took out of an agent's enabledModels.
type scopeGone struct {
	Entries []string `json:"entries,omitempty"`
	// the sync removed the list, which had nothing else
	Emptied bool `json:"emptied,omitempty"`
}

func scopeGoneLoad(key string) scopeGone {
	var g scopeGone
	if raw := stashShared()[key]; raw != "" {
		json.Unmarshal([]byte(raw), &g)
	}
	return g
}

func scopeGoneSave(key string, g scopeGone) {
	v := ""
	if len(g.Entries) > 0 {
		b, _ := json.Marshal(g)
		v = string(b)
	}
	stash(map[string]string{key: v})
}

// scopeGoneRename moves what a sync remembered off a renamed provider, as
// RenameRefs moves the list's own entries.
func scopeGoneRename(key, from, to string) {
	g := scopeGoneLoad(key)
	if next, ok := renameRefList(g.Entries, from, to); ok {
		g.Entries = next
		scopeGoneSave(key, g)
	}
}

// scopeUniSwitchOne is the model an entry names outright on uni-switch, lowercased
// and with its level off (bare cuts the level); "" for a glob or an entry on
// another provider.
func scopeUniSwitchOne(p string, bare func(string) string) string {
	p = strings.ToLower(bare(strings.TrimSpace(p)))
	if strings.ContainsAny(p, "*?[") || !strings.HasPrefix(p, uniSwitchID+"/") {
		return ""
	}
	return p
}

// piBare is a Pi entry without its thinking level.
func piBare(p string) string {
	if i := strings.LastIndex(p, ":"); i >= 0 && slices.Contains(piLevels, p[i+1:]) {
		return p[:i]
	}
	return p
}

// ompBare is an omp entry without its thinking level.
func ompBare(p string) string { m, _ := ompLevel(p); return m }

// scopeSynced is an enabledModels list, has false when there is none, once
// it has caught up with offered, the models uni-switch gives the agent as
// uni-switch/<id> lowercased: what goes, what comes back, and what is
// remembered after.
func scopeSynced(cur []string, has bool, offered map[string]bool, mem scopeGone, bare func(string) string) (next []string, nextHas bool, out scopeGone, changed bool) {
	if !has && !mem.Emptied {
		// no list, or one the user removed: nothing to take out or put back
		return cur, has, scopeGone{}, false
	}
	in := map[string]bool{}
	for _, p := range cur {
		one := scopeUniSwitchOne(p, bare)
		if one != "" && !offered[one] {
			if !slices.Contains(out.Entries, p) {
				out.Entries = append(out.Entries, p)
			}
			continue
		}
		next = append(next, p)
		if one != "" {
			in[one] = true
		}
	}
	for _, p := range mem.Entries {
		one := scopeUniSwitchOne(p, bare)
		switch {
		case one == "", in[one] && offered[one]:
			// the list names it again already
		case offered[one]:
			next = append(next, p)
			in[one] = true
		case !slices.Contains(out.Entries, p):
			out.Entries = append(out.Entries, p)
		}
	}
	nextHas = len(next) > 0
	out.Emptied = !nextHas && len(out.Entries) > 0
	changed = nextHas != has || !slices.Equal(next, cur)
	return next, nextHas, out, changed
}

// uniSwitchOffered is the models of uni-switch's provider block in a Pi-style
// models.json, as uni-switch/<id> lowercased; nil when the block or its list
// isn't there or can't be read, which says nothing about what is offered.
func uniSwitchOffered(modelsPath string) map[string]bool {
	raw, ok := edit.GetJSON(modelsPath, "providers."+uniSwitchID+".models")
	if !ok {
		return nil
	}
	var ms []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(raw), &ms) != nil {
		return nil
	}
	return offeredSet(len(ms), func(i int) string { return ms[i].ID })
}

func offeredSet(n int, id func(int) string) map[string]bool {
	out := map[string]bool{}
	for i := range n {
		if v := strings.TrimSpace(id(i)); v != "" {
			out[strings.ToLower(uniSwitchID+"/"+v)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// piScopeSynced catches Pi's enabledModels (settings.json) up with the
// models uni-switch's block in models.json gives it; mem is the stash key of
// what was taken out.
func piScopeSynced(settings, modelsPath, mem string) error {
	offered := uniSwitchOffered(modelsPath)
	if offered == nil {
		return nil
	}
	raw, has := edit.GetJSON(settings, "enabledModels")
	var cur []string
	if has && (json.Unmarshal([]byte(raw), &cur) != nil || len(cur) == 0) {
		// not a list of names, or an empty one: Pi's own reading of it
		// (piScope) holds nothing to, so neither does this
		return nil
	}
	next, nextHas, out, changed := scopeSynced(cur, has, offered, scopeGoneLoad(mem), piBare)
	if changed {
		var err error
		if nextHas {
			err = edit.SetJSON(settings, edit.KV{Path: "enabledModels", Value: next})
		} else {
			err = edit.DelJSON(settings, "enabledModels")
		}
		if err != nil {
			return err
		}
		// a list made again takes the model picked while there was none,
		// as piScopeWith would have had it
		if !has && nextHas {
			p, _ := edit.GetJSON(settings, "defaultProvider")
			m, _ := edit.GetJSON(settings, "defaultModel")
			if p != "" && m != "" {
				if err := piScopeWith(settings, p+"/"+m); err != nil {
					return err
				}
			}
		}
	}
	scopeGoneSave(mem, out)
	return nil
}

// stepcodeScopeSynced is piScopeSynced for StepCode's enabledModels
// (config.toml). The wiring state's last list follows when the list was
// still the one uni-switch last wrote, so a disconnect still knows it as
// uni-switch's and puts back the user's own.
func stepcodeScopeSynced(at place, path, modelsPath, mem string) error {
	offered := uniSwitchOffered(modelsPath)
	if offered == nil {
		return nil
	}
	cfg, err := readStepcodeTOML(path)
	if err != nil {
		return nil
	}
	var cur []string
	has := cfg.Scope != nil
	if has {
		if cur = *cfg.Scope; len(cur) == 0 {
			return nil
		}
	}
	next, nextHas, out, changed := scopeSynced(cur, has, offered, scopeGoneLoad(mem), piBare)
	if changed {
		if nextHas {
			if !has {
				if def := stepcodeStartup(cfg); def != "" && !slices.ContainsFunc(next, func(p string) bool { return piTakes(p, def) }) {
					next = append(next, def)
				}
			}
			raw, err := stepcodeScopeRaw(next)
			if err != nil {
				return err
			}
			err = edit.SetTOMLTopPreserving(path, edit.KV{Path: "enabledModels", Value: raw})
			if err != nil {
				return err
			}
		} else if err := edit.DelTOMLTop(path, "enabledModels"); err != nil {
			return err
		}
		if state, err := loadStepcodeState(at); err == nil && state != nil && state.LastHasScope == has && slices.Equal(state.LastScope, cur) {
			state.LastHasScope, state.LastScope = nextHas, slices.Clone(next)
			if err := stepcodeStoreState(at, state); err != nil {
				return err
			}
		}
	}
	scopeGoneSave(mem, out)
	return nil
}

// ompScopeSynced is piScopeSynced for omp's enabledModels (config.yml),
// whose plain entries alone it reads; an entry scoped to folders is the
// user's list for those projects and stays as it is. offered is what
// models.yml's uni-switch provider gives omp.
func ompScopeSynced(cfg, modelsYML, mem string) error {
	var doc struct {
		Providers map[string]struct {
			Models []struct {
				ID string `yaml:"id"`
			} `yaml:"models"`
		} `yaml:"providers"`
	}
	raw, err := edit.Read(modelsYML)
	if err != nil || raw == nil || yaml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	ms := doc.Providers[uniSwitchID].Models
	offered := offeredSet(len(ms), func(i int) string { return ms[i].ID })
	if offered == nil {
		return nil
	}
	g := scopeGoneLoad(mem)
	var out scopeGone
	var made []string
	err = edit.EditYAMLNode(cfg, "enabledModels", func(n *yaml.Node) (bool, bool) {
		if n == nil {
			if g.Emptied {
				made, _, out, _ = scopeSynced(nil, false, offered, g, ompBare)
			}
			return false, false
		}
		if n.Kind != yaml.SequenceNode {
			out = g
			return false, false
		}
		plain := ompScopePlain(n)
		if len(plain) == 0 && !g.Emptied {
			// none holds a session to them (ompScopeTakes); one put here
			// would, unless the sync took the plain entries there were
			out = g
			return false, false
		}
		next, _, o, changed := scopeSynced(plain, true, offered, g, ompBare)
		out = o
		if !changed {
			return false, false
		}
		gone := map[string]bool{}
		for _, p := range plain {
			if !slices.Contains(next, p) {
				gone[p] = true
			}
		}
		kept := n.Content[:0:0]
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && c.ShortTag() == "!!str" && gone[c.Value] {
				continue
			}
			kept = append(kept, c)
		}
		for _, p := range next {
			if !slices.Contains(plain, p) {
				kept = append(kept, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p})
			}
		}
		out.Emptied = !slices.ContainsFunc(kept, func(c *yaml.Node) bool { return c.Kind == yaml.ScalarNode && c.ShortTag() == "!!str" }) && len(out.Entries) > 0
		if len(kept) == 0 {
			return false, true
		}
		n.Content = kept
		return true, false
	})
	if err != nil {
		return err
	}
	if len(made) > 0 {
		if err := edit.SetYAML(cfg, edit.KV{Path: "enabledModels", Value: made}); err != nil {
			return err
		}
	}
	scopeGoneSave(mem, out)
	return nil
}
